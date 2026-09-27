package returns

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/sales"
)

type Service struct {
	repo      Repository
	branches  BranchLookup
	salesRepo SalesReader
	inventory InventoryWriter
	audit     AuditWriter
	db        common.Transactioner
}

func NewService(repo Repository, branches BranchLookup, salesRepo SalesReader, inv InventoryWriter, auditWriter AuditWriter, db common.Transactioner) *Service {
	return &Service{repo: repo, branches: branches, salesRepo: salesRepo, inventory: inv, audit: auditWriter, db: db}
}

var _ Interface = (*Service)(nil)

// resolveBranchIDs turns branchID (one verified branch, or every branch in
// orgID when nil) into the branchIDs Repository's List queries expect -
// same reasoning as inventory.Service.resolveBranchIDs (Void/Return/
// Exchange carry no BranchID of their own either).
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

// applyStockDelta is returns' own copy of the get-or-create-then-adjust
// pattern behind every stock movement in this codebase (same shape as
// inventory/procurement/sales' own copies).
func (s *Service) applyStockDelta(tx *gorm.DB, productID uint, branchID uint, delta int) (int, error) {
	level, err := s.inventory.GetStockLevel(tx, productID, branchID)
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
		if err := s.inventory.CreateStockLevel(tx, &inventory.StockLevel{ProductID: productID, BranchID: branchID, Qty: newQty}); err != nil {
			return 0, err
		}
	} else if err := s.inventory.UpdateStockLevelQty(tx, level.ID, newQty); err != nil {
		return 0, err
	}
	return newQty, nil
}

// VoidSale reverses the entire sale - see the Interface doc. actor.StaffID
// is captured as ApprovedBy regardless of whether CanApprove came from the
// staff's own permission or a manager's approval token, same reasoning as
// sales.CreateSale's actor handling.
func (s *Service) VoidSale(ctx context.Context, orgID uint, actor Actor, saleID uuid.UUID, in VoidSaleRequest) (*Void, error) {
	if !actor.CanApprove {
		return nil, common.ForbiddenError("staff does not have permission to approve a void")
	}

	var v Void
	err := s.db.Transaction(func(tx *gorm.DB) error {
		sale, err := s.salesRepo.GetSaleWithLock(tx, orgID, saleID)
		if err != nil {
			return err
		}
		if sale.Status == sales.SaleStatusVoided {
			return common.ConflictError("this sale is already voided")
		}
		if err := s.salesRepo.RequireOpenShift(tx, orgID, sale.BranchID, sale.ShiftID); err != nil {
			return common.ConflictError("this sale's shift is no longer open - a void is only valid within the same shift it was rung up in")
		}
		hasReturnOrExchange, err := s.repo.SaleHasReturnOrExchange(tx, saleID)
		if err != nil {
			return err
		}
		if hasReturnOrExchange {
			return common.ConflictError("this sale already has a return or exchange against it and can no longer be voided as a whole")
		}
		items, err := s.salesRepo.ListSaleItemsTx(tx, saleID)
		if err != nil {
			return err
		}

		totalQty := 0
		qtyByProduct := make(map[uint]int, len(items))
		for _, item := range items {
			totalQty += item.Qty
			qtyByProduct[item.ProductID] += item.Qty
		}

		v = Void{
			SaleID:      saleID,
			Qty:         totalQty,
			Reason:      in.Reason,
			Explanation: in.Explanation,
			ApprovedBy:  actor.StaffID,
		}
		if err := s.repo.CreateVoid(tx, &v); err != nil {
			return err
		}

		voidRef := strconv.FormatUint(uint64(v.ID), 10)
		for productID, qty := range qtyByProduct {
			newQty, err := s.applyStockDelta(tx, productID, sale.BranchID, qty)
			if err != nil {
				return err
			}
			if err := s.inventory.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeVoid, Qty: qty, BalanceAfter: newQty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeVoid, ReferenceID: voidRef,
			}); err != nil {
				return err
			}
		}

		if err := s.salesRepo.UpdateSaleStatus(tx, saleID, sales.SaleStatusVoided); err != nil {
			return err
		}

		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: &actor.StaffID, BranchID: &sale.BranchID,
			Entity: "sale", EntityID: saleID.String(), Action: "voided",
			Before: audit.ToJSON(sale),
			After:  audit.ToJSON(map[string]any{"status": sales.SaleStatusVoided, "void_id": v.ID, "qty_reversed": totalQty}),
		})
	})
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) ListVoids(ctx context.Context, orgID uint, branchID *uint) ([]Void, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListVoids(ctx, branchIDs)
}

// CreateReturn confirms the sale is completed and every item belongs to it
// with enough not-yet-returned-or-exchanged-in qty remaining, computes
// RefundTotal as each returned item's own original per-unit price (derived
// from SaleItem.LineTotal, so it's already net of that line's own discount)
// times the qty being returned, and restocks each Condition
// ItemConditionSellable item at the sale's own branch - all atomically.
func (s *Service) CreateReturn(ctx context.Context, orgID uint, actor Actor, in CreateReturnRequest) (*Return, error) {
	if !actor.CanApprove {
		return nil, common.ForbiddenError("staff does not have permission to approve a return")
	}

	var ret Return
	err := s.db.Transaction(func(tx *gorm.DB) error {
		sale, err := s.salesRepo.GetSaleWithLock(tx, orgID, in.SaleID)
		if err != nil {
			return err
		}
		if sale.Status != sales.SaleStatusCompleted {
			return common.ConflictError("only a completed sale can be returned")
		}
		saleItems, err := s.salesRepo.ListSaleItemsTx(tx, in.SaleID)
		if err != nil {
			return err
		}
		bySaleItemID := make(map[uint]sales.SaleItem, len(saleItems))
		for _, si := range saleItems {
			bySaleItemID[si.ID] = si
		}

		refundTotal := decimal.Zero
		returnItems := make([]ReturnItem, 0, len(in.Items))
		creditByProduct := make(map[uint]int)
		for _, reqItem := range in.Items {
			saleItem, ok := bySaleItemID[reqItem.SaleItemID]
			if !ok {
				return common.BadRequestError("sale_item_id does not belong to this sale")
			}
			alreadyReturned, err := s.repo.ReturnedQtyForSaleItem(tx, reqItem.SaleItemID)
			if err != nil {
				return err
			}
			alreadyExchanged, err := s.repo.ExchangedInQtyForSaleItem(tx, reqItem.SaleItemID)
			if err != nil {
				return err
			}
			if alreadyReturned+alreadyExchanged+reqItem.Qty > saleItem.Qty {
				return common.BadRequestError("return qty exceeds what remains returnable for this item")
			}

			unitRefund := saleItem.LineTotal.Div(decimal.NewFromInt(int64(saleItem.Qty)))
			lineRefund := unitRefund.Mul(decimal.NewFromInt(int64(reqItem.Qty))).Round(2)
			refundTotal = refundTotal.Add(lineRefund)

			restocked := reqItem.Condition == ItemConditionSellable
			returnItems = append(returnItems, ReturnItem{
				SaleItemID: reqItem.SaleItemID,
				Qty:        reqItem.Qty,
				Condition:  reqItem.Condition,
				Restocked:  restocked,
			})
			if restocked {
				creditByProduct[saleItem.ProductID] += reqItem.Qty
			}
		}

		ret = Return{
			SaleID:       in.SaleID,
			ReasonCode:   in.ReasonCode,
			RefundMethod: in.RefundMethod,
			RefundTotal:  refundTotal,
			ApprovedBy:   actor.StaffID,
		}
		if err := s.repo.CreateReturn(tx, &ret); err != nil {
			return err
		}
		for i := range returnItems {
			returnItems[i].ReturnID = ret.ID
		}
		if err := s.repo.CreateReturnItems(tx, returnItems); err != nil {
			return err
		}

		returnRef := strconv.FormatUint(uint64(ret.ID), 10)
		for productID, qty := range creditByProduct {
			newQty, err := s.applyStockDelta(tx, productID, sale.BranchID, qty)
			if err != nil {
				return err
			}
			if err := s.inventory.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeReturn, Qty: qty, BalanceAfter: newQty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeReturn, ReferenceID: returnRef,
			}); err != nil {
				return err
			}
		}

		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: &actor.StaffID, BranchID: &sale.BranchID,
			Entity: "sale", EntityID: in.SaleID.String(), Action: "returned",
			Before: audit.ToJSON(nil), After: audit.ToJSON(ret),
		})
	})
	if err != nil {
		return nil, err
	}
	return &ret, nil
}

func (s *Service) ListReturns(ctx context.Context, orgID uint, branchID *uint) ([]Return, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListReturns(ctx, branchIDs)
}

func (s *Service) GetReturn(ctx context.Context, orgID uint, id uint) (*Return, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, nil)
	if err != nil {
		return nil, err
	}
	return s.repo.GetReturn(ctx, branchIDs, id)
}

// CreateExchange confirms the sale is completed, computes NetDifference as
// (every "out" item's qty*unit_price) minus (every "in" item's own original
// per-unit sale price*qty) - positive means the customer owes more,
// negative means they're owed the difference - credits stock for every
// "in" item and decrements it for every "out" item at the sale's own
// branch, all atomically. Modeled as one combined transaction record
// (Exchange/ExchangeItem), not a chained Return-then-Sale.
func (s *Service) CreateExchange(ctx context.Context, orgID uint, actor Actor, in CreateExchangeRequest) (*Exchange, error) {
	if !actor.CanApprove {
		return nil, common.ForbiddenError("staff does not have permission to approve an exchange")
	}

	var ex Exchange
	err := s.db.Transaction(func(tx *gorm.DB) error {
		sale, err := s.salesRepo.GetSaleWithLock(tx, orgID, in.SaleID)
		if err != nil {
			return err
		}
		if sale.Status != sales.SaleStatusCompleted {
			return common.ConflictError("only a completed sale can be exchanged")
		}
		saleItems, err := s.salesRepo.ListSaleItemsTx(tx, in.SaleID)
		if err != nil {
			return err
		}
		bySaleItemID := make(map[uint]sales.SaleItem, len(saleItems))
		for _, si := range saleItems {
			bySaleItemID[si.ID] = si
		}

		netDifference := decimal.Zero
		exchangeItems := make([]ExchangeItem, 0, len(in.Items))
		creditByProduct := make(map[uint]int)
		debitByProduct := make(map[uint]int)

		for _, reqItem := range in.Items {
			switch reqItem.Direction {
			case DirectionIn:
				if reqItem.SaleItemID == nil {
					return common.BadRequestError("sale_item_id is required for an \"in\" item")
				}
				saleItem, ok := bySaleItemID[*reqItem.SaleItemID]
				if !ok {
					return common.BadRequestError("sale_item_id does not belong to this sale")
				}
				alreadyReturned, err := s.repo.ReturnedQtyForSaleItem(tx, *reqItem.SaleItemID)
				if err != nil {
					return err
				}
				alreadyExchanged, err := s.repo.ExchangedInQtyForSaleItem(tx, *reqItem.SaleItemID)
				if err != nil {
					return err
				}
				if alreadyReturned+alreadyExchanged+reqItem.Qty > saleItem.Qty {
					return common.BadRequestError("exchanged-in qty exceeds what remains returnable for this item")
				}

				unitPrice := saleItem.LineTotal.Div(decimal.NewFromInt(int64(saleItem.Qty))).Round(2)
				lineValue := unitPrice.Mul(decimal.NewFromInt(int64(reqItem.Qty)))
				netDifference = netDifference.Sub(lineValue)
				creditByProduct[saleItem.ProductID] += reqItem.Qty

				productID := saleItem.ProductID
				exchangeItems = append(exchangeItems, ExchangeItem{
					Direction: DirectionIn, SaleItemID: reqItem.SaleItemID, ProductID: &productID,
					Qty: reqItem.Qty, UnitPrice: unitPrice,
				})
			case DirectionOut:
				if reqItem.ProductID == nil || reqItem.UnitPrice == nil {
					return common.BadRequestError("product_id and unit_price are required for an \"out\" item")
				}
				lineValue := reqItem.UnitPrice.Mul(decimal.NewFromInt(int64(reqItem.Qty))).Round(2)
				netDifference = netDifference.Add(lineValue)
				debitByProduct[*reqItem.ProductID] += reqItem.Qty

				exchangeItems = append(exchangeItems, ExchangeItem{
					Direction: DirectionOut, ProductID: reqItem.ProductID,
					Qty: reqItem.Qty, UnitPrice: *reqItem.UnitPrice,
				})
			default:
				return common.BadRequestError("direction must be \"in\" or \"out\"")
			}
		}

		ex = Exchange{SaleID: in.SaleID, NetDifference: netDifference, ApprovedBy: actor.StaffID}
		if err := s.repo.CreateExchange(tx, &ex); err != nil {
			return err
		}
		for i := range exchangeItems {
			exchangeItems[i].ExchangeID = ex.ID
		}
		if err := s.repo.CreateExchangeItems(tx, exchangeItems); err != nil {
			return err
		}

		exchangeRef := strconv.FormatUint(uint64(ex.ID), 10)
		for productID, qty := range creditByProduct {
			newQty, err := s.applyStockDelta(tx, productID, sale.BranchID, qty)
			if err != nil {
				return err
			}
			if err := s.inventory.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeExchangeIn, Qty: qty, BalanceAfter: newQty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeExchange, ReferenceID: exchangeRef,
			}); err != nil {
				return err
			}
		}
		for productID, qty := range debitByProduct {
			newQty, err := s.applyStockDelta(tx, productID, sale.BranchID, -qty)
			if err != nil {
				return err
			}
			if err := s.inventory.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeExchangeOut, Qty: -qty, BalanceAfter: newQty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeExchange, ReferenceID: exchangeRef,
			}); err != nil {
				return err
			}
		}

		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: &actor.StaffID, BranchID: &sale.BranchID,
			Entity: "sale", EntityID: in.SaleID.String(), Action: "exchanged",
			Before: audit.ToJSON(nil), After: audit.ToJSON(ex),
		})
	})
	if err != nil {
		return nil, err
	}
	return &ex, nil
}

func (s *Service) ListExchanges(ctx context.Context, orgID uint, branchID *uint) ([]Exchange, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListExchanges(ctx, branchIDs)
}

func (s *Service) GetExchange(ctx context.Context, orgID uint, id uint) (*Exchange, error) {
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, nil)
	if err != nil {
		return nil, err
	}
	return s.repo.GetExchange(ctx, branchIDs, id)
}
