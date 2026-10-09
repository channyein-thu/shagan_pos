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

// fullyReturned reports whether, counting the lines being returned now
// (requestedQty) on top of what earlier returns took, every line of the sale
// has been returned in full. Exchange "in" qty deliberately doesn't count: a
// line that was swapped rather than refunded keeps the sale "completed".
// returnedBefore holds the prior returned qty already looked up for the
// requested lines; any other line is looked up here.
func (s *Service) fullyReturned(tx *gorm.DB, saleItems []sales.SaleItem, returnedBefore map[uint]int, requestedQty map[uint]int) (bool, error) {
	if len(saleItems) == 0 {
		return false, nil
	}
	for _, si := range saleItems {
		before, known := returnedBefore[si.ID]
		if !known {
			var err error
			before, err = s.repo.ReturnedQtyForSaleItem(tx, si.ID)
			if err != nil {
				return false, err
			}
		}
		if before+requestedQty[si.ID] < si.Qty {
			return false, nil
		}
	}
	return true, nil
}

// recordWriteoff writes the ledger row for goods that came back but were not
// restocked: type return_writeoff, qty 0, and the product's balance as it
// stands (0 when it has no stock row yet - none is created, stock is
// untouched). Call it after any restock in the same transaction so the
// balance it reports includes that restock.
func (s *Service) recordWriteoff(tx *gorm.DB, orgID uint, branchID uint, productID uint, actorID *uint, refType inventory.ReferenceType, refID string) error {
	level, err := s.inventory.GetStockLevel(tx, productID, branchID)
	if err != nil {
		return err
	}
	balance := 0
	if level != nil {
		balance = level.Qty
	}
	return s.inventory.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
		OrgID: orgID, ProductID: productID, BranchID: branchID,
		Type: inventory.LedgerEntryTypeReturnWriteoff, Qty: 0, BalanceAfter: balance,
		ActorID: actorID, ReferenceType: refType, ReferenceID: refID,
	})
}

// currentShiftID returns the open shift of the till the request came from,
// to stamp on a Return/Exchange so Close Shift can account for the cash that
// left the drawer. Nil when there's no till context or no open shift - the
// record is still written, it just isn't counted in any shift's expected
// cash.
func (s *Service) currentShiftID(tx *gorm.DB, orgID uint, actor Actor) (*uint, error) {
	if actor.PosUserID == 0 {
		return nil, nil
	}
	return s.repo.CurrentShiftID(tx, orgID, actor.PosUserID)
}

// VoidSale reverses the entire sale - see the Interface doc. actor.StaffID
// is captured as ApprovedBy regardless of whether CanApprove came from the
// staff's own permission or a manager's approval token, same reasoning as
// sales.CreateSale's actor handling. An Owner voiding directly (actor.UserID,
// no StaffID) is captured as ApprovedByUserID instead; the audit entry
// records them as ActorUserID, and the inventory ledger rows leave actor_id
// nil (its reference_id points at the void, which names the Owner).
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
		if sale.Status == sales.SaleStatusRefunded {
			return common.ConflictError("this sale has been fully returned (refunded) and can no longer be voided")
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
			SaleID:           saleID,
			Qty:              totalQty,
			Reason:           in.Reason,
			Explanation:      in.Explanation,
			ApprovedBy:       actor.staffIDPtr(),
			ApprovedByUserID: actor.userIDPtr(),
		}
		if err := s.repo.CreateVoid(tx, &v); err != nil {
			return err
		}

		voidRef := strconv.FormatUint(uint64(v.ID), 10)
		for productID, qty := range qtyByProduct {
			if _, err := inventory.ApplyMovement(tx, s.inventory, inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeVoid, Qty: qty,
				ActorID: actor.staffIDPtr(), ReferenceType: inventory.ReferenceTypeVoid, ReferenceID: voidRef,
			}, inventory.RejectNegativeStock); err != nil {
				return err
			}
		}

		if err := s.salesRepo.UpdateSaleStatus(tx, saleID, sales.SaleStatusVoided); err != nil {
			return err
		}

		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, ActorID: actor.staffIDPtr(), ActorUserID: actor.userIDPtr(), BranchID: &sale.BranchID,
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
// RefundTotal through the shared cumulative proportional valuation (derived
// from SaleItem.LineTotal, already net of its discount), and restocks each Condition
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
		incoming := make([]incomingItem, len(in.Items))
		for i, item := range in.Items {
			incoming[i] = incomingItem{SaleItemID: item.SaleItemID, Qty: item.Qty, Condition: item.Condition}
		}
		plan, err := s.planIncomingItems(tx, saleItems, incoming)
		if err != nil {
			return err
		}
		returnItems := make([]ReturnItem, len(plan.Items))
		for i, item := range plan.Items {
			returnItems[i] = ReturnItem{
				SaleItemID: item.SaleItemID, Qty: item.Qty,
				Condition: item.Condition, Restocked: item.Restocked,
			}
		}

		shiftID, err := s.currentShiftID(tx, orgID, actor)
		if err != nil {
			return err
		}

		ret = Return{
			SaleID:       in.SaleID,
			ReasonCode:   in.ReasonCode,
			Explanation:  in.Explanation,
			RefundMethod: in.RefundMethod,
			RefundTotal:  plan.Total,
			ApprovedBy:   actor.StaffID,
			ShiftID:      shiftID,
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
		for productID, qty := range plan.CreditByProduct {
			if _, err := inventory.ApplyMovement(tx, s.inventory, inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeReturn, Qty: qty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeReturn, ReferenceID: returnRef,
			}, inventory.RejectNegativeStock); err != nil {
				return err
			}
		}
		// Every line back through returns alone settles the sale: mark it
		// refunded so history shows it. Void rejects such a sale, and the
		// shift totals keep counting its original payments.
		fullyReturned, err := s.fullyReturned(tx, saleItems, plan.ReturnedBefore, plan.RequestedQty)
		if err != nil {
			return err
		}
		if fullyReturned {
			if err := s.salesRepo.UpdateSaleStatus(tx, in.SaleID, sales.SaleStatusRefunded); err != nil {
				return err
			}
		}
		// Goods that weren't restocked still left the customer and must show
		// in the ledger - one write-off row per such line, after the restocks
		// so the balance it reports includes them.
		for _, productID := range plan.WriteoffProducts {
			if err := s.recordWriteoff(tx, orgID, sale.BranchID, productID, &actor.StaffID, inventory.ReferenceTypeReturn, returnRef); err != nil {
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
// (every "out" item's qty*unit_price) minus the incoming plan's credit,
// using the same rounding allocation as returns. Positive means the customer owes more,
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
		var incoming []incomingItem
		for _, item := range in.Items {
			if item.Direction != DirectionIn {
				continue
			}
			if item.SaleItemID == nil {
				return common.BadRequestError("sale_item_id is required for an \"in\" item")
			}
			condition := item.Condition
			if condition == "" {
				condition = ItemConditionSellable
			}
			incoming = append(incoming, incomingItem{SaleItemID: *item.SaleItemID, Qty: item.Qty, Condition: condition})
		}
		plan, err := s.planIncomingItems(tx, saleItems, incoming)
		if err != nil {
			return err
		}
		netDifference := plan.Total.Neg()
		exchangeItems := make([]ExchangeItem, 0, len(in.Items))
		debitByProduct := make(map[uint]int)
		incomingIndex := 0

		for _, reqItem := range in.Items {
			switch reqItem.Direction {
			case DirectionIn:
				item := plan.Items[incomingIndex]
				incomingIndex++
				exchangeItems = append(exchangeItems, ExchangeItem{
					Direction: DirectionIn, SaleItemID: &item.SaleItemID, ProductID: &item.ProductID,
					Qty: item.Qty, UnitPrice: item.UnitPrice, Condition: &item.Condition,
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

		// A non-zero difference moved money, and Close Shift needs to know
		// whether that was through the drawer - so say how it was settled.
		if !netDifference.IsZero() && in.Method == "" {
			return common.BadRequestError("method (cash or qr) is required when the exchange has a non-zero net difference")
		}
		var method *ExchangeMethod
		if in.Method != "" {
			m := in.Method
			method = &m
		}
		shiftID, err := s.currentShiftID(tx, orgID, actor)
		if err != nil {
			return err
		}

		ex = Exchange{SaleID: in.SaleID, NetDifference: netDifference, ApprovedBy: actor.StaffID, Method: method, ShiftID: shiftID}
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
		for productID, qty := range plan.CreditByProduct {
			if _, err := inventory.ApplyMovement(tx, s.inventory, inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeExchangeIn, Qty: qty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeExchange, ReferenceID: exchangeRef,
			}, inventory.RejectNegativeStock); err != nil {
				return err
			}
		}
		for productID, qty := range debitByProduct {
			if _, err := inventory.ApplyMovement(tx, s.inventory, inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: sale.BranchID,
				Type: inventory.LedgerEntryTypeExchangeOut, Qty: -qty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeExchange, ReferenceID: exchangeRef,
			}, inventory.RejectNegativeStock); err != nil {
				return err
			}
		}
		// Non-sellable "in" goods moved no stock but still came back: record
		// them, after the credits and debits so the balance is the final one.
		for _, productID := range plan.WriteoffProducts {
			if err := s.recordWriteoff(tx, orgID, sale.BranchID, productID, &actor.StaffID, inventory.ReferenceTypeExchange, exchangeRef); err != nil {
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
