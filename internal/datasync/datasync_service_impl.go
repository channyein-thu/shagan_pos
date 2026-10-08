package datasync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/sales"
)

type Service struct {
	repo     Repository
	branches BranchLookup
	staff    StaffLookup
	catalog  CatalogReader
	sales    SalesWriter
	db       common.Transactioner
}

func NewService(repo Repository, branches BranchLookup, staff StaffLookup, catalogReader CatalogReader, salesWriter SalesWriter, db common.Transactioner) *Service {
	return &Service{repo: repo, branches: branches, staff: staff, catalog: catalogReader, sales: salesWriter, db: db}
}

var _ Interface = (*Service)(nil)

// resolveBranchIDs turns branchID (one verified branch, or every branch in
// orgID when nil) into the branchIDs Repository's scoped queries expect -
// same reasoning as inventory.Service.resolveBranchIDs.
func (s *Service) resolveBranchIDs(ctx context.Context, orgID uint, branchID *uint) ([]uint, error) {
	if branchID != nil {
		if _, err := s.branches.GetBranch(ctx, orgID, *branchID); err != nil {
			return nil, err
		}
		return []uint{*branchID}, nil
	}
	branchList, err := s.branches.ListBranches(ctx, orgID)
	if err != nil {
		return nil, err
	}
	branchIDs := make([]uint, len(branchList))
	for i, b := range branchList {
		branchIDs[i] = b.ID
	}
	return branchIDs, nil
}

func (s *Service) GetCatalogSnapshot(ctx context.Context, orgID uint) (*CatalogSnapshot, string, error) {
	products, err := s.catalog.ListProducts(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	categories, err := s.catalog.ListCategories(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	combos, err := s.catalog.ListCombos(ctx, orgID)
	if err != nil {
		return nil, "", err
	}
	snapshot := &CatalogSnapshot{Products: products, Categories: categories, Combos: combos}

	etag, err := etagFor(snapshot)
	if err != nil {
		return nil, "", common.SystemError("failed to build catalog snapshot")
	}
	return snapshot, etag, nil
}

// etagFor hashes a stable view of the snapshot - every image's URL is
// blanked out first, since it's a freshly re-signed, deliberately
// short-lived link (see storage.Storage.PresignedURL) that differs on
// every single call regardless of whether the underlying image changed.
// Hashing it directly would mean the ETag never matches twice, defeating
// If-None-Match caching entirely. ID/Width/Height (which only change when
// the image itself is actually replaced) are kept.
func etagFor(snapshot *CatalogSnapshot) (string, error) {
	stable := *snapshot
	stable.Products = make([]catalog.ProductResult, len(snapshot.Products))
	copy(stable.Products, snapshot.Products)
	for i := range stable.Products {
		stable.Products[i].Images = blankImageURLs(stable.Products[i].Images)
	}
	stable.Combos = make([]catalog.ComboResult, len(snapshot.Combos))
	copy(stable.Combos, snapshot.Combos)
	for i := range stable.Combos {
		stable.Combos[i].Images = blankComboImageURLs(stable.Combos[i].Images)
	}

	body, err := json.Marshal(&stable)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`, nil
}

func blankImageURLs(images []catalog.ProductImageResult) []catalog.ProductImageResult {
	out := make([]catalog.ProductImageResult, len(images))
	for i, img := range images {
		img.URL = ""
		out[i] = img
	}
	return out
}

func blankComboImageURLs(images []catalog.ComboImageResult) []catalog.ComboImageResult {
	out := make([]catalog.ComboImageResult, len(images))
	for i, img := range images {
		img.URL = ""
		out[i] = img
	}
	return out
}

// IngestQueuedSales processes each queued sale independently - one item
// failing (or conflicting) never stops the rest from being ingested. See
// the Interface doc for the idempotency/negative-stock/pre-authorization
// rules.
func (s *Service) IngestQueuedSales(ctx context.Context, orgID uint, branchID uint, in IngestQueuedSalesRequest) (*IngestQueuedSalesResponse, error) {
	results := make([]IngestSaleResult, 0, len(in.Sales))
	for _, req := range in.Sales {
		result := IngestSaleResult{SaleID: req.ID.String()}

		// Already arrived? Checked before any validation below so a retry of
		// an ingested sale stays a success even if its staff has since moved
		// or left.
		if existing, err := s.sales.GetSale(ctx, orgID, req.ID); err == nil && existing != nil {
			if existing.SameOrigin(branchID, req.DeviceID) {
				result.Success = true
			} else {
				result.Error = "a sale with this id already exists"
			}
			results = append(results, result)
			continue
		}

		if err := s.validateQueuedSale(ctx, orgID, branchID, req); err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		// No manual-discount authority is ever granted on this path - see
		// validateQueuedSale; false here is the backstop behind it.
		actor := sales.SaleActor{StaffID: req.StaffID, CanApplyManualDiscount: false}
		_, negativeEvents, err := s.sales.CreateSale(ctx, orgID, branchID, actor, req, true)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		result.Success = true
		results = append(results, result)

		for _, ev := range negativeEvents {
			payload, marshalErr := json.Marshal(ev)
			if marshalErr != nil {
				continue
			}
			_ = s.repo.CreateSyncConflict(ctx, &SyncConflict{
				SaleID:  req.ID,
				Reason:  SyncConflictReasonStockConflict,
				Payload: payload,
			})
		}
	}
	return &IngestQueuedSalesResponse{Results: results}, nil
}

func (s *Service) GetSyncStatus(ctx context.Context, orgID uint, branchID *uint) (*SyncStatus, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	unresolvedCount, err := s.repo.UnresolvedConflictCount(ctx, branchIDs)
	if err != nil {
		return nil, err
	}
	lastSyncedAt, err := s.repo.LastSyncedAt(ctx, branchIDs)
	if err != nil {
		return nil, err
	}
	return &SyncStatus{UnresolvedConflictCount: unresolvedCount, LastSyncedAt: lastSyncedAt}, nil
}

func (s *Service) ListSyncConflicts(ctx context.Context, orgID uint, branchID *uint) ([]SyncConflict, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListSyncConflicts(ctx, branchIDs)
}

func (s *Service) ResolveSyncConflict(ctx context.Context, orgID uint, actorUserID uint, id uint) (*SyncConflict, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, nil)
	if err != nil {
		return nil, err
	}

	var conflict SyncConflict
	err = s.db.Transaction(func(tx *gorm.DB) error {
		found, err := s.repo.GetSyncConflictWithLock(tx, branchIDs, id)
		if err != nil {
			return err
		}
		conflict = *found
		if conflict.ResolvedAt != nil {
			return common.ConflictError("this conflict is already resolved")
		}
		now := time.Now()
		if err := s.repo.UpdateSyncConflictResolved(tx, id, actorUserID, now); err != nil {
			return err
		}
		conflict.ResolvedBy = &actorUserID
		conflict.ResolvedAt = &now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &conflict, nil
}

// validateQueuedSale enforces what a queued sale can't prove for itself (it
// carries no staff token): no item discount - an offline till can't get a
// manager's approval - and a staff_id that is a real staff member of the
// calling device's own branch. Returns a message-only error for the failed
// IngestSaleResult.
func (s *Service) validateQueuedSale(ctx context.Context, orgID uint, branchID uint, req sales.CreateSaleRequest) error {
	for _, item := range req.Items {
		if item.Discount.IsPositive() {
			return common.BadRequestError("an offline-queued sale cannot carry a manual discount")
		}
	}
	const notYourStaff = "staff_id is not a staff member of this branch"
	if req.StaffID == 0 {
		return common.BadRequestError(notYourStaff)
	}
	staff, err := s.staff.GetStaff(ctx, orgID, req.StaffID)
	if err != nil {
		var restErr common.RestError
		if errors.As(err, &restErr) && restErr.Status == http.StatusNotFound {
			return common.BadRequestError(notYourStaff)
		}
		return err
	}
	if staff.BranchID != branchID {
		return common.BadRequestError(notYourStaff)
	}
	return nil
}
