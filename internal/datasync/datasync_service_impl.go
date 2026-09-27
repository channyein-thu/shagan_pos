package datasync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/sales"
)

type Service struct {
	repo     Repository
	branches BranchLookup
	catalog  CatalogReader
	sales    SalesWriter
	db       common.Transactioner
}

func NewService(repo Repository, branches BranchLookup, catalogReader CatalogReader, salesWriter SalesWriter, db common.Transactioner) *Service {
	return &Service{repo: repo, branches: branches, catalog: catalogReader, sales: salesWriter, db: db}
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

func (s *Service) GetCatalogSnapshot(ctx context.Context, orgID uint, branchID *uint) (*CatalogSnapshot, string, error) {
	products, err := s.catalog.ListProducts(ctx, orgID, branchID)
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

		if existing, err := s.sales.GetSale(ctx, orgID, req.ID); err == nil && existing != nil {
			result.Success = true
			results = append(results, result)
			continue
		}

		actor := sales.SaleActor{StaffID: req.StaffID, CanApplyManualDiscount: true}
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
