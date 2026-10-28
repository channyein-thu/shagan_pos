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
	orgs      OrganizationLookup
	returns   ReturnActivityReader
	staff     StaffDirectory
	db        common.Transactioner
}

func NewService(repo Repository, inv InventoryWriter, products ProductLookup, auditWriter AuditWriter, orgs OrganizationLookup, returns ReturnActivityReader, staff StaffDirectory, db common.Transactioner) *Service {
	return &Service{repo: repo, inventory: inv, products: products, audit: auditWriter, orgs: orgs, returns: returns, staff: staff, db: db}
}

var _ Interface = (*Service)(nil)

// pricedLine is one request line after the server has priced it from the
// product: nothing in it comes from the client except qty.
type pricedLine struct {
	item     SaleItem
	approval *DiscountApproval // the approval this line spends, if any
	mismatch *PriceMismatch    // queued sales only: the till's cache was stale for this line
}

// CreateSale prices every line from the PRODUCT (see CreateSaleItemRequest),
// derives Subtotal/Discount/Tax/Total from those lines rather than trusting
// client-computed totals, rejects a payments total that doesn't equal the
// derived Total, then persists the Sale/SaleItems/Payments as one atomic unit
// guarded by a currently-open-shift check - same transaction-composes-atomic-
// primitives shape as identity.Service.CreateAccount.
//
// A line's discount is one of three things, never mixed:
//   - the product's catalog % (automatic: no approval, no permission),
//   - a custom % taken from the ORIGINAL price that REPLACES the catalog %,
//     carried by a DiscountApproval the seller got when the discount was added
//     (the approval is checked here, spent atomically, and audited), or
//   - on a combo line, the client's share of the bundle discount.
//
// There is no permission or manager-token check for discounts here any more:
// the approval record is the authorization, and payment never asks for a PIN.
//
// allowNegativeStock is only ever true for an offline-queued sale
// (datasync.IngestQueuedSales). That sale was priced from the till's cached
// catalog, so its unit price and catalog % are taken as rung up - a stale one
// is reported on Sale.PriceMismatches, not rejected - and it can never carry
// an approval (there is no approval offline).
//
// Also decrements each product's stock at branchID (the selling branch,
// aggregated across every line for that product) in the same transaction,
// rejecting (409) if any product doesn't have enough - branchID, not the
// product's own origin branch, since a product can be sellable at more than
// one branch after a completed stock transfer (see inventory's own
// CreateStockTransfer doc). Every item's stock effect and the sale itself
// commit or roll back together.
func (s *Service) CreateSale(ctx context.Context, orgID uint, branchID uint, actor SaleActor, in CreateSaleRequest, allowNegativeStock bool) (*Sale, []NegativeStockEvent, error) {
	// Idempotent by in.ID - checked before anything else, so a till retrying
	// after a lost response gets its original sale back even if the discount
	// approval it carried has since been used (by this very sale).
	existing, err := s.repo.GetSale(ctx, orgID, in.ID)
	switch {
	case err == nil:
		return s.replay(existing, branchID, in.DeviceID)
	case !isNotFound(err):
		return nil, nil, err
	}

	queued := allowNegativeStock
	lines, err := s.priceLines(ctx, orgID, branchID, actor, in, queued)
	if err != nil {
		return nil, nil, err
	}

	saleItems := make([]SaleItem, len(lines))
	subtotal, discountTotal, taxTotal := decimal.Zero, decimal.Zero, decimal.Zero
	for i, line := range lines {
		saleItems[i] = line.item
		subtotal = subtotal.Add(line.item.UnitPrice.Mul(decimal.NewFromInt(int64(line.item.Qty))))
		discountTotal = discountTotal.Add(line.item.Discount)
		taxTotal = taxTotal.Add(line.item.Tax)
	}
	// Prices already contain their tax, so it is never added: the customer pays
	// exactly the discounted price. taxTotal is just the part that is tax.
	total := subtotal.Sub(discountTotal)

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

	now := time.Now()
	sale := &Sale{
		ID:           in.ID,
		OrgID:        orgID,
		BranchID:     branchID,
		ShiftID:      in.ShiftID,
		StaffID:      actor.StaffID,
		DeviceID:     in.DeviceID,
		CustomerID:   in.CustomerID,
		Subtotal:     subtotal,
		Discount:     discountTotal,
		Tax:          taxTotal,
		TaxInclusive: true,
		Total:        total,
		Status:       SaleStatusCompleted,
		CompletedAt:  &now,
	}
	// allowNegativeStock is only ever true for datasync.Service.IngestQueuedSales
	// re-submitting an offline-queued sale - stamp SyncedAt to mark it as
	// confirmed received, same reasoning as the Interface doc. A live,
	// online-rung-up sale was never "synced" from anywhere, so it stays nil.
	if queued {
		sale.SyncedAt = &now
		for _, line := range lines {
			if line.mismatch != nil {
				sale.PriceMismatches = append(sale.PriceMismatches, *line.mismatch)
			}
		}
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
		// Spend each approval inside the sale's transaction. The conditional
		// update is what makes "once" hold when two sales race for one
		// approval; a loser rolls the whole sale back.
		for _, line := range lines {
			if line.approval == nil {
				continue
			}
			won, err := s.repo.MarkDiscountApprovalUsed(tx, line.approval.ID, sale.ID)
			if err != nil {
				return err
			}
			if !won {
				return common.ConflictError("this discount approval was already used")
			}
			if err := s.audit.CreateAuditLog(tx, &audit.AuditLog{
				OrgID: orgID, ActorID: &actor.StaffID, BranchID: &branchID,
				Entity: "sale", EntityID: sale.ID.String(), Action: "manual_discount",
				Before: audit.ToJSON(nil),
				After: audit.ToJSON(map[string]any{
					"discount_approval_id": line.approval.ID, "product_id": line.item.ProductID,
					"percent": line.approval.Percent, "qty": line.item.Qty, "manual_discount": line.item.ManualDiscount,
					"seller": line.approval.RequestedBy, "approver": line.approval.ApprovedBy, "reason": line.approval.Reason,
				}),
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

// unitSalePrice is the unit price after the catalog or manual discount (not the
// combo share, which is the bundle's, not the unit's): UnitPrice minus the
// per-unit amount of whichever of the two applied. A line sold before the
// discount was split by source has neither, so it reads as its original price.
func unitSalePrice(item SaleItem) decimal.Decimal {
	if item.Qty <= 0 {
		return item.UnitPrice
	}
	perUnit := item.CatalogDiscount.Add(item.ManualDiscount).Div(decimal.NewFromInt(int64(item.Qty)))
	return item.UnitPrice.Sub(perUnit)
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

func (s *Service) ListSales(ctx context.Context, orgID uint, branchID *uint, staffID *uint, shiftID *uint, from, to *time.Time, page, pageSize int) (*SalesPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	f := SaleFilter{BranchID: branchID, StaffID: staffID, ShiftID: shiftID, Page: page, PageSize: pageSize}
	if from != nil || to != nil {
		// The dates are days of the org's own calendar, not UTC's.
		org, err := s.orgs.GetOrganization(ctx, orgID)
		if err != nil {
			return nil, err
		}
		loc := common.OrgLocation(org.Timezone)
		if from != nil {
			start := common.StartOfDay(*from, loc)
			f.Start = &start
		}
		if to != nil {
			// to is an inclusive calendar date: its whole day counts.
			end := common.NextDay(common.StartOfDay(*to, loc))
			f.End = &end
		}
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
	// A sale nobody returned or exchanged has no entry: its zero
	// ReturnSummary (false, false, "0") is exactly the right answer.
	var summaries map[uuid.UUID]ReturnSummary
	if len(ids) > 0 {
		summaries, err = s.returns.ReturnSummaries(ctx, ids)
		if err != nil {
			return nil, err
		}
	}

	items := make([]SaleListItem, len(sales))
	for i, sale := range sales {
		m := methods[sale.ID]
		if m == nil {
			m = []PaymentMethod{}
		}
		items[i] = SaleListItem{Sale: sale, ReturnSummary: summaries[sale.ID], PaymentMethods: m}
	}
	return &SalesPage{Sales: items, Page: page, PageSize: pageSize, TotalCount: total}, nil
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

	// returned_qty counts return lines and exchange "in" lines - the same two
	// sources the over-return guards in returns.CreateReturn/CreateExchange
	// enforce - so returnable_qty is exactly what the backend will still
	// accept, and the till can stop offering more before a manager's PIN.
	itemIDs := make([]uint, len(items))
	for i, item := range items {
		itemIDs[i] = item.ID
	}
	returnedQty, err := s.returns.ReturnedQtyBySaleItems(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	summaries, err := s.returns.ReturnSummaries(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	// Who approved each manual discount, by name: one lookup per approver, not
	// per line. An approver who can no longer be found just has no name.
	approverNames := make(map[uint]string)
	for _, item := range items {
		if item.ManualApprovedBy == nil {
			continue
		}
		approver := *item.ManualApprovedBy
		if _, done := approverNames[approver]; done {
			continue
		}
		staff, err := s.staff.GetStaff(ctx, orgID, approver)
		switch {
		case err == nil:
			approverNames[approver] = staff.Name
		case isNotFound(err):
			approverNames[approver] = ""
		default:
			return nil, err
		}
	}
	receiptItems := make([]ReceiptItem, len(items))
	for i, item := range items {
		returned := returnedQty[item.ID]
		returnable := item.Qty - returned
		if returnable < 0 {
			returnable = 0
		}
		ri := ReceiptItem{SaleItem: item, ReturnedQty: returned, ReturnableQty: returnable, UnitSalePrice: unitSalePrice(item)}
		if item.ManualApprovedBy != nil {
			ri.ManualApprovedByName = approverNames[*item.ManualApprovedBy]
		}
		receiptItems[i] = ri
	}
	return map[string]any{
		"sale":     ReceiptSale{Sale: *sale, ReturnSummary: summaries[id]},
		"items":    receiptItems,
		"payments": payments,
		// true: the prices include the tax (print "Includes tax"). false is a
		// sale from before, whose tax was added on top of its total.
		"tax_inclusive": sale.TaxInclusive,
	}, nil
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
