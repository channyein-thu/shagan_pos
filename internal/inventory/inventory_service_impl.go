package inventory

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Service struct {
	repo     Repository
	branches BranchLookup
	products ProductLookup
	db       common.Transactioner
}

func NewService(repo Repository, branches BranchLookup, products ProductLookup, db common.Transactioner) *Service {
	return &Service{repo: repo, branches: branches, products: products, db: db}
}

var _ Interface = (*Service)(nil)

// resolveBranchIDs turns branchID (one verified branch, or every branch in
// orgID when nil) into the branchIDs Repository queries expect - shared by
// every method that needs to scope a query against StockLevel/StockTransfer,
// neither of which carries its own OrgID column. See ListStockLevels's
// original doc for why this ownership check exists at all.
func (s *Service) resolveBranchIDs(ctx context.Context, orgID uint, branchID *uint) ([]uint, error) {
	if branchID != nil {
		if _, err := s.branches.GetBranch(ctx, orgID, *branchID); err != nil {
			return nil, err
		}
		return []uint{*branchID}, nil
	}
	branches, err := s.branches.ListBranches(ctx, orgID)
	if err != nil {
		return nil, err
	}
	branchIDs := make([]uint, len(branches))
	for i, b := range branches {
		branchIDs[i] = b.ID
	}
	return branchIDs, nil
}

// ListStockLevels resolves branchID (see the interface doc) into the set of
// branch IDs to query - one verified branch, or every branch in orgID -
// before delegating to the repository, since StockLevel has no OrgID
// column of its own to filter by directly.
func (s *Service) ListStockLevels(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]StockLevel, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListStockLevels(ctx, branchIDs, productID)
}

func (s *Service) ListLowStock(ctx context.Context, orgID uint, branchID *uint) ([]StockLevel, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListLowStock(ctx, branchIDs)
}

func (s *Service) ListInventoryLedger(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]InventoryLedger, error) {
	return s.repo.ListInventoryLedger(ctx, orgID, branchID, productID)
}

// CreateStockAdjustment confirms in.BranchID belongs to orgID and
// in.ProductID belongs to orgID (products are org-wide, see catalog.Product's
// doc, so BranchID is a required client choice rather than derived from the
// product), then applies Delta to that product's StockLevel at that branch
// and appends one InventoryLedger entry, all atomically.
func (s *Service) CreateStockAdjustment(ctx context.Context, orgID uint, actorID uint, in CreateStockAdjustmentRequest) (*StockAdjustment, error) {
	if in.UnitCost != nil && in.Delta <= 0 {
		return nil, common.BadRequestError("unit_cost is only meaningful for a positive delta")
	}
	if in.UnitCost != nil && in.UnitCost.IsNegative() {
		return nil, common.BadRequestError("unit_cost must be zero or greater")
	}
	if _, err := s.branches.GetBranch(ctx, orgID, in.BranchID); err != nil {
		return nil, err
	}
	product, err := s.products.GetProduct(ctx, orgID, in.ProductID)
	if err != nil {
		return nil, err
	}

	var adjustment StockAdjustment
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// Create the reference first; it rolls back if movement or valuation fails.
		adjustment = StockAdjustment{
			ProductID: in.ProductID,
			BranchID:  in.BranchID,
			Delta:     in.Delta,
			Reason:    in.Reason,
			ActorID:   actorID,
		}
		if err := s.repo.CreateStockAdjustment(tx, &adjustment); err != nil {
			return err
		}

		newQty, err := ApplyMovement(tx, s.repo, InventoryLedger{
			OrgID: orgID, ProductID: in.ProductID, BranchID: in.BranchID,
			Type: LedgerEntryTypeAdjustment, Qty: in.Delta, ActorID: &actorID,
			ReferenceType: ReferenceTypeAdjustment,
			ReferenceID:   strconv.FormatUint(uint64(adjustment.ID), 10),
		}, RejectNegativeStock)
		if err != nil {
			return err
		}
		if in.UnitCost != nil {
			currentQty := newQty - in.Delta
			newCost := WeightedAverageCost(currentQty, product.CostPrice, in.Delta, *in.UnitCost)
			if err := s.products.UpdateProduct(tx, in.ProductID, map[string]any{"cost_price": newCost}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &adjustment, nil
}

func (s *Service) ListStockTransfers(ctx context.Context, orgID uint, branchID *uint) ([]StockTransferResult, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	transfers, err := s.repo.ListStockTransfers(ctx, branchIDs)
	if err != nil {
		return nil, err
	}
	results := make([]StockTransferResult, len(transfers))
	if len(transfers) == 0 {
		return results, nil
	}

	ids := make([]uint, len(transfers))
	for i, t := range transfers {
		ids[i] = t.ID
	}
	lines, err := s.repo.ListStockTransferItemsByTransferIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byTransfer := make(map[uint][]StockTransferItem, len(transfers))
	for _, line := range lines {
		byTransfer[line.TransferID] = append(byTransfer[line.TransferID], line)
	}
	for i, t := range transfers {
		results[i] = newStockTransferResult(t, byTransfer[t.ID])
	}
	return results, nil
}

// newStockTransferResult pairs a transfer with its lines; Items is an empty
// list, never nil, when there are none.
func newStockTransferResult(t StockTransfer, lines []StockTransferItem) StockTransferResult {
	items := make([]StockTransferItemResult, len(lines))
	for i, l := range lines {
		items[i] = StockTransferItemResult{ProductID: l.ProductID, Qty: l.Qty}
	}
	return StockTransferResult{StockTransfer: t, Items: items}
}

// CreateStockTransfer confirms FromBranch/ToBranch both belong to orgID and
// differ, every item's product belongs to orgID, and FromBranch's
// StockLevel covers each item's qty - see the interface doc for why
// (products are org-wide, see catalog.Product's doc, so there's no
// product-belongs-to-a-branch check anymore, only a stock-sufficiency one).
// The sufficiency check runs inside the same transaction as creation, not
// before it, so it can't race with another movement against the same
// StockLevel row. No stock actually moves here though; only
// UpdateStockTransfer completing it does - this is a soft, point-in-time
// sanity check, not the real enforcement (ApplyMovement's own
// negative-qty guard is what protects completion time, since stock can
// still change while a transfer sits pending).
func (s *Service) CreateStockTransfer(ctx context.Context, orgID uint, actorID uint, in CreateStockTransferRequest) (*StockTransferResult, error) {
	if in.FromBranch == in.ToBranch {
		return nil, common.BadRequestError("from_branch and to_branch must be different")
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > 500 {
		return nil, common.BadRequestError("note must be at most 500 characters")
	}
	if _, err := s.branches.GetBranch(ctx, orgID, in.FromBranch); err != nil {
		return nil, err
	}
	if _, err := s.branches.GetBranch(ctx, orgID, in.ToBranch); err != nil {
		return nil, err
	}
	for _, item := range in.Items {
		if _, err := s.products.GetProduct(ctx, orgID, item.ProductID); err != nil {
			return nil, err
		}
	}

	var transfer StockTransfer
	var items []StockTransferItem
	err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, item := range in.Items {
			level, err := s.repo.GetStockLevel(tx, item.ProductID, in.FromBranch)
			if err != nil {
				return err
			}
			available := 0
			if level != nil {
				available = level.Qty
			}
			if available < item.Qty {
				return common.ConflictError("insufficient stock at from_branch for this transfer")
			}
		}

		transfer = StockTransfer{
			FromBranch: in.FromBranch,
			ToBranch:   in.ToBranch,
			Status:     TransferStatusPending,
			ActorID:    actorID,
			Note:       note,
		}
		if err := s.repo.CreateStockTransfer(tx, &transfer); err != nil {
			return err
		}
		items = make([]StockTransferItem, len(in.Items))
		for i, item := range in.Items {
			items[i] = StockTransferItem{TransferID: transfer.ID, ProductID: item.ProductID, Qty: item.Qty}
		}
		return s.repo.CreateStockTransferItems(tx, items)
	})
	if err != nil {
		return nil, err
	}
	result := newStockTransferResult(transfer, items)
	return &result, nil
}

// UpdateStockTransfer confirms the transfer exists AND belongs to orgID and
// isn't already terminal (Completed/Cancelled), then applies the status
// change - see the interface doc for what completing one actually does.
func (s *Service) UpdateStockTransfer(ctx context.Context, orgID uint, id uint, in UpdateStockTransferRequest) (*StockTransferResult, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, nil)
	if err != nil {
		return nil, err
	}

	var transfer StockTransfer
	var items []StockTransferItem
	err = s.db.Transaction(func(tx *gorm.DB) error {
		found, err := s.repo.GetStockTransfer(tx, branchIDs, id, true)
		if err != nil {
			return err
		}
		transfer = *found
		if transfer.Status == TransferStatusCompleted || transfer.Status == TransferStatusCancelled {
			return common.ConflictError("this transfer is already completed or cancelled")
		}

		// Read the lines for every transition: completing moves them, and
		// every response carries them.
		items, err = s.repo.ListStockTransferItems(tx, transfer.ID)
		if err != nil {
			return err
		}

		if in.Status == TransferStatusCompleted {
			for _, item := range items {
				transferRef := strconv.FormatUint(uint64(transfer.ID), 10)
				if _, err := ApplyMovement(tx, s.repo, InventoryLedger{
					OrgID: orgID, ProductID: item.ProductID, BranchID: transfer.FromBranch,
					Type: LedgerEntryTypeTransferOut, Qty: -item.Qty,
					ActorID: &transfer.ActorID, ReferenceType: ReferenceTypeStockTransfer, ReferenceID: transferRef,
				}, RejectNegativeStock); err != nil {
					return err
				}
				if _, err := ApplyMovement(tx, s.repo, InventoryLedger{
					OrgID: orgID, ProductID: item.ProductID, BranchID: transfer.ToBranch,
					Type: LedgerEntryTypeTransferIn, Qty: item.Qty,
					ActorID: &transfer.ActorID, ReferenceType: ReferenceTypeStockTransfer, ReferenceID: transferRef,
				}, RejectNegativeStock); err != nil {
					return err
				}
			}
		}

		if err := s.repo.UpdateStockTransferStatus(tx, id, in.Status); err != nil {
			return err
		}
		transfer.Status = in.Status
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := newStockTransferResult(transfer, items)
	return &result, nil
}
