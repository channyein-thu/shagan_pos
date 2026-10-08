package sales

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
)

type Service struct {
	repo      Repository
	inventory InventoryWriter
	products  ProductLookup
	audit     AuditWriter
	db        common.Transactioner
}

func NewService(repo Repository, inv InventoryWriter, products ProductLookup, auditWriter AuditWriter, db common.Transactioner) *Service {
	return &Service{repo: repo, inventory: inv, products: products, audit: auditWriter, db: db}
}

var _ Interface = (*Service)(nil)

// CreateSale derives Subtotal/Discount/Tax/Total from in.Items rather than
// trusting client-computed totals, rejects a payments total that doesn't
// cover the derived Total, then persists the Sale/SaleItems/Payments as one
// atomic unit guarded by a currently-open-shift check - same
// transaction-composes-atomic-primitives shape as identity.Service.CreateAccount.
//
// A discount on any item requires actor.CanApplyManualDiscount - a plain
// Staff member's own token won't have it (only super_staff/manager do, per
// the seeded role grants); it's true either because the calling staff holds
// apply_manual_discount themselves, or because a manager approved this sale
// via X-Manager-Approval-Token (see middleware.ManagerApproved) - both are
// folded into one bool by the handler before this is ever called, so this
// method doesn't need to know which case applied.
//
// Also decrements each product's stock at branchID (the selling branch,
// aggregated across every line for that product) in the same transaction,
// rejecting (409) if any product doesn't have enough - branchID, not the
// product's own origin branch, since a product can be sellable at more than
// one branch after a completed stock transfer (see inventory's own
// CreateStockTransfer doc). Every item's stock effect and the sale itself
// commit or roll back together.
func (s *Service) CreateSale(ctx context.Context, orgID uint, branchID uint, actor SaleActor, in CreateSaleRequest, allowNegativeStock bool) (*Sale, []NegativeStockEvent, error) {
	// Idempotent by in.ID - checked before anything else (including the
	// discount-permission check), so a till retrying after a lost response
	// gets its original sale back even if the manager-approval token that
	// authorized it has long expired.
	existing, err := s.repo.GetSale(ctx, orgID, in.ID)
	switch {
	case err == nil:
		return s.replay(existing, branchID, in.DeviceID)
	case !isNotFound(err):
		return nil, nil, err
	}

	saleItems := make([]SaleItem, 0, len(in.Items))
	subtotal := decimal.Zero
	itemDiscountTotal := decimal.Zero
	itemTaxTotal := decimal.Zero
	for _, item := range in.Items {
		if item.Discount.IsNegative() {
			return nil, nil, common.BadRequestError("item discount must be zero or greater")
		}
		if item.Tax.IsNegative() {
			return nil, nil, common.BadRequestError("item tax must be zero or greater")
		}
		effectivePrice := item.UnitPrice
		if item.PriceOverride != nil {
			effectivePrice = *item.PriceOverride
		}
		lineGross := effectivePrice.Mul(decimal.NewFromInt(int64(item.Qty)))
		subtotal = subtotal.Add(lineGross)
		itemDiscountTotal = itemDiscountTotal.Add(item.Discount)
		itemTaxTotal = itemTaxTotal.Add(item.Tax)
		saleItems = append(saleItems, SaleItem{
			SaleID:        in.ID,
			ProductID:     item.ProductID,
			ComboID:       item.ComboID,
			NameSnapshot:  item.NameSnapshot,
			UnitPrice:     item.UnitPrice,
			PriceOverride: item.PriceOverride,
			Qty:           item.Qty,
			LineTotal:     lineGross.Sub(item.Discount),
			Discount:      item.Discount,
			Tax:           item.Tax,
		})
	}
	if itemDiscountTotal.IsPositive() && !actor.CanApplyManualDiscount {
		return nil, nil, common.ForbiddenError("staff does not have permission to apply a manual discount")
	}
	total := subtotal.Sub(itemDiscountTotal).Add(itemTaxTotal)

	payments := make([]Payment, 0, len(in.Payments))
	paymentsTotal := decimal.Zero
	for _, p := range in.Payments {
		paymentsTotal = paymentsTotal.Add(p.Amount)
		payments = append(payments, Payment{
			SaleID:         in.ID,
			Method:         p.Method,
			Amount:         p.Amount,
			AmountReceived: p.AmountReceived,
			ChangeGiven:    p.ChangeGiven,
		})
	}
	if !paymentsTotal.Equal(total) {
		return nil, nil, common.BadRequestError("payments must add up to the sale total")
	}

	// Cost is resolved server-side, once per unique product, only once the
	// request itself is known to be well-formed - see SaleItem.UnitCost's
	// own doc for why this is never client-supplied.
	costByProduct := make(map[uint]decimal.Decimal, len(saleItems))
	for i := range saleItems {
		productID := saleItems[i].ProductID
		cost, ok := costByProduct[productID]
		if !ok {
			product, err := s.products.GetProduct(ctx, orgID, productID)
			if err != nil {
				return nil, nil, err
			}
			cost = product.CostPrice
			costByProduct[productID] = cost
		}
		saleItems[i].UnitCost = cost
	}

	now := time.Now()
	sale := &Sale{
		ID:          in.ID,
		OrgID:       orgID,
		BranchID:    branchID,
		ShiftID:     in.ShiftID,
		StaffID:     actor.StaffID,
		DeviceID:    in.DeviceID,
		CustomerID:  in.CustomerID,
		Subtotal:    subtotal,
		Discount:    itemDiscountTotal,
		Tax:         itemTaxTotal,
		Total:       total,
		Status:      SaleStatusCompleted,
		CompletedAt: &now,
	}
	// allowNegativeStock is only ever true for datasync.Service.IngestQueuedSales
	// re-submitting an offline-queued sale - stamp SyncedAt to mark it as
	// confirmed received, same reasoning as the Interface doc. A live,
	// online-rung-up sale was never "synced" from anywhere, so it stays nil.
	if allowNegativeStock {
		sale.SyncedAt = &now
	}

	qtyByProduct := make(map[uint]int, len(in.Items))
	for _, item := range in.Items {
		qtyByProduct[item.ProductID] += item.Qty
	}

	var negativeEvents []NegativeStockEvent
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.RequireOpenShift(tx, orgID, branchID, in.ShiftID); err != nil {
			return err
		}
		if err := s.repo.CreateSale(tx, sale); err != nil {
			return err
		}
		if err := s.repo.CreateSaleItems(tx, saleItems); err != nil {
			return err
		}
		if err := s.repo.CreatePayments(tx, payments); err != nil {
			return err
		}
		for productID, qty := range qtyByProduct {
			newQty, err := s.applyStockDelta(tx, productID, branchID, -qty, allowNegativeStock)
			if err != nil {
				return err
			}
			if newQty < 0 {
				negativeEvents = append(negativeEvents, NegativeStockEvent{ProductID: productID, BranchID: branchID, ResultingQty: newQty})
			}
			if err := s.inventory.CreateInventoryLedgerEntry(tx, &inventory.InventoryLedger{
				OrgID: orgID, ProductID: productID, BranchID: branchID,
				Type: inventory.LedgerEntryTypeSale, Qty: -qty, BalanceAfter: newQty,
				ActorID: &actor.StaffID, ReferenceType: inventory.ReferenceTypeSale, ReferenceID: sale.ID.String(),
			}); err != nil {
				return err
			}
		}
		if itemDiscountTotal.IsPositive() {
			if err := s.audit.CreateAuditLog(tx, &audit.AuditLog{
				OrgID: orgID, ActorID: &actor.StaffID, BranchID: &branchID,
				Entity: "sale", EntityID: sale.ID.String(), Action: "manual_discount_applied",
				Before: audit.ToJSON(nil), After: audit.ToJSON(sale),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// Two retries of the same sale racing past the check above: the
		// loser's insert hits the primary key. Resolve it the same way as
		// a sequential retry instead of surfacing a 500.
		if common.IsDuplicateError(err) {
			if existing, getErr := s.repo.GetSale(ctx, orgID, in.ID); getErr == nil {
				return s.replay(existing, branchID, in.DeviceID)
			}
			// Not in this org: the id belongs to someone else's sale.
			return nil, nil, common.ConflictError("a sale with this id already exists")
		}
		return nil, nil, err
	}
	return sale, negativeEvents, nil
}

// replay answers a CreateSale whose id is already stored: the original sale
// back (flagged Replayed) when it was rung up at this same branch and
// device, a 409 otherwise - a different till reusing a client UUID is a
// bug or an attack, never a retry, and mustn't hand over the other sale.
func (s *Service) replay(existing *Sale, branchID, deviceID uint) (*Sale, []NegativeStockEvent, error) {
	if !existing.SameOrigin(branchID, deviceID) {
		return nil, nil, common.ConflictError("a sale with this id already exists")
	}
	existing.Replayed = true
	return existing, nil, nil
}

func isNotFound(err error) bool {
	var restErr common.RestError
	return errors.As(err, &restErr) && restErr.Status == http.StatusNotFound
}

// applyStockDelta is sales' own copy of the get-or-create-then-adjust
// pattern behind every stock movement in this codebase (same shape as
// inventory.Service/procurement.Service's own copies) - resolves the
// current qty (0 if no StockLevel row exists yet for this product/branch
// pair), applies delta, and persists the new value. Returns
// common.ConflictError if applying delta would take qty negative.
func (s *Service) applyStockDelta(tx *gorm.DB, productID uint, branchID uint, delta int, allowNegative bool) (int, error) {
	level, err := s.inventory.GetStockLevel(tx, productID, branchID)
	if err != nil {
		return 0, err
	}
	current := 0
	if level != nil {
		current = level.Qty
	}
	newQty := current + delta
	if newQty < 0 && !allowNegative {
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

func (s *Service) ListSales(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, page, pageSize int) (*SalesPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	f := SaleFilter{BranchID: branchID, Page: page, PageSize: pageSize}
	if from != nil {
		start := utcDay(*from)
		f.Start = &start
	}
	if to != nil {
		// to is an inclusive calendar date: its whole day counts.
		end := utcDay(*to).Add(24 * time.Hour)
		f.End = &end
	}

	sales, total, err := s.repo.ListSales(ctx, orgID, f)
	if err != nil {
		return nil, err
	}

	ids := make([]uuid.UUID, len(sales))
	for i, sale := range sales {
		ids[i] = sale.ID
	}
	methods, err := s.repo.ListPaymentMethods(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]SaleListItem, len(sales))
	for i, sale := range sales {
		m := methods[sale.ID]
		if m == nil {
			m = []PaymentMethod{}
		}
		items[i] = SaleListItem{Sale: sale, PaymentMethods: m}
	}
	return &SalesPage{Sales: items, Page: page, PageSize: pageSize, TotalCount: total}, nil
}

func utcDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (s *Service) GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*Sale, error) {
	return s.repo.GetSale(ctx, orgID, id)
}

func (s *Service) GetSaleReceipt(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error) {
	sale, err := s.repo.GetSale(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListSaleItems(ctx, id)
	if err != nil {
		return nil, err
	}
	payments, err := s.repo.ListPayments(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sale": sale, "items": items, "payments": payments}, nil
}

func (s *Service) ReprintSale(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error) {
	return s.GetSaleReceipt(ctx, orgID, id)
}

// CreateHeldSale parks the calling staff's current cart. BranchID/StaffID
// come from the caller's own verified tokens, never in - see
// cmd/api/sales.go's CreateHeldSale handler.
func (s *Service) CreateHeldSale(ctx context.Context, branchID uint, staffID uint, in CreateHeldSaleRequest) (*HeldSale, error) {
	if in.Discount.IsNegative() {
		return nil, common.BadRequestError("discount must be zero or greater")
	}
	in.BranchID = branchID
	in.StaffID = staffID
	in.HeldAt = time.Now()
	return s.repo.CreateHeldSale(ctx, in)
}

func (s *Service) ListHeldSales(ctx context.Context, branchID uint) ([]HeldSale, error) {
	return s.repo.ListHeldSales(ctx, branchID)
}

func (s *Service) ResumeHeldSale(ctx context.Context, branchID uint, id uint) (*HeldSale, error) {
	return s.repo.ResumeHeldSale(ctx, branchID, id)
}
