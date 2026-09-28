package inventory

import (
	"context"
	"strconv"

	"github.com/shopspring/decimal"
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

// CreateStockAdjustment confirms in.ProductID belongs to orgID and takes the
// product's own BranchID as the adjustment's branch (see the interface
// doc), then applies Delta to that product's StockLevel and appends one
// InventoryLedger entry, all atomically.
func (s *Service) CreateStockAdjustment(ctx context.Context, orgID uint, actorID uint, in CreateStockAdjustmentRequest) (*StockAdjustment, error) {
	if in.UnitCost != nil && in.Delta <= 0 {
		return nil, common.BadRequestError("unit_cost is only meaningful for a positive delta")
	}
	if in.UnitCost != nil && in.UnitCost.IsNegative() {
		return nil, common.BadRequestError("unit_cost must be zero or greater")
	}
	product, err := s.products.GetProduct(ctx, orgID, in.ProductID)
	if err != nil {
		return nil, err
	}

	var adjustment StockAdjustment
	err = s.db.Transaction(func(tx *gorm.DB) error {
		newQty, err := s.applyStockDelta(tx, in.ProductID, product.BranchID, in.Delta)
		if err != nil {
			return err
		}

		if in.UnitCost != nil {
			currentQty := newQty - in.Delta
			newCost := weightedAverageCost(currentQty, product.CostPrice, in.Delta, *in.UnitCost)
			if err := s.products.UpdateProduct(tx, in.ProductID, map[string]any{"cost_price": newCost}); err != nil {
				return err
			}
		}

		adjustment = StockAdjustment{
			ProductID: in.ProductID,
			BranchID:  product.BranchID,
			Delta:     in.Delta,
			Reason:    in.Reason,
			ActorID:   actorID,
		}
		if err := s.repo.CreateStockAdjustment(tx, &adjustment); err != nil {
			return err
		}

		return s.repo.CreateInventoryLedgerEntry(tx, &InventoryLedger{
			OrgID:         orgID,
			ProductID:     in.ProductID,
			BranchID:      product.BranchID,
			Type:          LedgerEntryTypeAdjustment,
			Qty:           in.Delta,
			BalanceAfter:  newQty,
			ActorID:       &actorID,
			ReferenceType: ReferenceTypeAdjustment,
			ReferenceID:   strconv.FormatUint(uint64(adjustment.ID), 10),
		})
	})
	if err != nil {
		return nil, err
	}
	return &adjustment, nil
}

// applyStockDelta is the shared find-or-create-then-adjust step behind both
// CreateStockAdjustment and UpdateStockTransfer's completion path -
// resolves the current qty (0 if no StockLevel row exists yet for this
// product/branch pair), applies delta, and persists the new value. Returns
// common.ConflictError if applying delta would take qty negative - stock
// can never go below zero from a movement this domain controls (see
// docs/WORKFLOWS.md's stock-decrement rule).
func (s *Service) applyStockDelta(tx *gorm.DB, productID uint, branchID uint, delta int) (int, error) {
	level, err := s.repo.GetStockLevel(tx, productID, branchID)
	if err != nil {
		return 0, err
	}
	current := 0
	if level != nil {
		current = level.Qty
	}
	newQty := current + delta
	if newQty < 0 {
		return 0, common.ConflictError("insufficient stock for this movement")
	}
	if level == nil {
		if err := s.repo.CreateStockLevel(tx, &StockLevel{ProductID: productID, BranchID: branchID, Qty: newQty}); err != nil {
			return 0, err
		}
	} else if err := s.repo.UpdateStockLevelQty(tx, level.ID, newQty); err != nil {
		return 0, err
	}
	return newQty, nil
}

// weightedAverageCost blends receivedQty units at receivedUnitCost into a
// product's existing cost basis, weighted by currentQty - see
// procurement.Service's own copy of this exact function for the full doc
// (same small-helper-duplicated-per-domain shape as applyStockDelta).
func weightedAverageCost(currentQty int, currentCost decimal.Decimal, receivedQty int, receivedUnitCost decimal.Decimal) decimal.Decimal {
	if currentQty <= 0 {
		return receivedUnitCost
	}
	existingValue := currentCost.Mul(decimal.NewFromInt(int64(currentQty)))
	receivedValue := receivedUnitCost.Mul(decimal.NewFromInt(int64(receivedQty)))
	totalQty := decimal.NewFromInt(int64(currentQty + receivedQty))
	return existingValue.Add(receivedValue).Div(totalQty).Round(2)
}

func (s *Service) ListStockTransfers(ctx context.Context, orgID uint, branchID *uint) ([]StockTransfer, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListStockTransfers(ctx, branchIDs)
}

// CreateStockTransfer confirms FromBranch/ToBranch both belong to orgID and
// differ, and every item's product belongs to orgID AND to FromBranch
// specifically - see the interface doc for why. No stock movement happens
// here; only UpdateStockTransfer completing it does.
func (s *Service) CreateStockTransfer(ctx context.Context, orgID uint, actorID uint, in CreateStockTransferRequest) (*StockTransfer, error) {
	if in.FromBranch == in.ToBranch {
		return nil, common.BadRequestError("from_branch and to_branch must be different")
	}
	if _, err := s.branches.GetBranch(ctx, orgID, in.FromBranch); err != nil {
		return nil, err
	}
	if _, err := s.branches.GetBranch(ctx, orgID, in.ToBranch); err != nil {
		return nil, err
	}
	for _, item := range in.Items {
		product, err := s.products.GetProduct(ctx, orgID, item.ProductID)
		if err != nil {
			return nil, err
		}
		if product.BranchID != in.FromBranch {
			return nil, common.BadRequestError("product does not belong to from_branch")
		}
	}

	transfer := StockTransfer{
		FromBranch: in.FromBranch,
		ToBranch:   in.ToBranch,
		Status:     TransferStatusPending,
		ActorID:    actorID,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.CreateStockTransfer(tx, &transfer); err != nil {
			return err
		}
		items := make([]StockTransferItem, len(in.Items))
		for i, item := range in.Items {
			items[i] = StockTransferItem{TransferID: transfer.ID, ProductID: item.ProductID, Qty: item.Qty}
		}
		return s.repo.CreateStockTransferItems(tx, items)
	})
	if err != nil {
		return nil, err
	}
	return &transfer, nil
}

// UpdateStockTransfer confirms the transfer exists AND belongs to orgID and
// isn't already terminal (Completed/Cancelled), then applies the status
// change - see the interface doc for what completing one actually does.
func (s *Service) UpdateStockTransfer(ctx context.Context, orgID uint, id uint, in UpdateStockTransferRequest) (*StockTransfer, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, nil)
	if err != nil {
		return nil, err
	}

	var transfer StockTransfer
	err = s.db.Transaction(func(tx *gorm.DB) error {
		found, err := s.repo.GetStockTransfer(tx, branchIDs, id, true)
		if err != nil {
			return err
		}
		transfer = *found
		if transfer.Status == TransferStatusCompleted || transfer.Status == TransferStatusCancelled {
			return common.ConflictError("this transfer is already completed or cancelled")
		}

		if in.Status == TransferStatusCompleted {
			items, err := s.repo.ListStockTransferItems(tx, transfer.ID)
			if err != nil {
				return err
			}
			for _, item := range items {
				newFromQty, err := s.applyStockDelta(tx, item.ProductID, transfer.FromBranch, -item.Qty)
				if err != nil {
					return err
				}
				newToQty, err := s.applyStockDelta(tx, item.ProductID, transfer.ToBranch, item.Qty)
				if err != nil {
					return err
				}
				transferRef := strconv.FormatUint(uint64(transfer.ID), 10)
				if err := s.repo.CreateInventoryLedgerEntry(tx, &InventoryLedger{
					OrgID: orgID, ProductID: item.ProductID, BranchID: transfer.FromBranch,
					Type: LedgerEntryTypeTransferOut, Qty: -item.Qty, BalanceAfter: newFromQty,
					ActorID: &transfer.ActorID, ReferenceType: ReferenceTypeStockTransfer, ReferenceID: transferRef,
				}); err != nil {
					return err
				}
				if err := s.repo.CreateInventoryLedgerEntry(tx, &InventoryLedger{
					OrgID: orgID, ProductID: item.ProductID, BranchID: transfer.ToBranch,
					Type: LedgerEntryTypeTransferIn, Qty: item.Qty, BalanceAfter: newToQty,
					ActorID: &transfer.ActorID, ReferenceType: ReferenceTypeStockTransfer, ReferenceID: transferRef,
				}); err != nil {
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
	return &transfer, nil
}
