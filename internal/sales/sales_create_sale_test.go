package sales

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
)

// saleFixture is a Service wired to mocks, selling at branch 3 on shift 9 as
// staff 14 from device 3.
type saleFixture struct {
	svc      *Service
	repo     *MockRepository
	inv      *MockInventoryWriter
	audW     *MockAuditWriter
	products *MockProductLookup
	combos   *MockComboLookup
}

func newSaleFixture(t *testing.T) *saleFixture {
	t.Helper()
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	combos := NewMockComboLookup(t)
	svc := newTestService(repo, inv, products, audW)
	svc.combos = combos
	return &saleFixture{svc: svc, repo: repo, inv: inv, audW: audW, products: products, combos: combos}
}

// catalogProduct is a product the way the catalog stores it: sale price already
// derived from its catalog percent.
func catalogProduct(id uint, price, percent string) *catalog.Product {
	p, pct := d(price), d(percent)
	return &catalog.Product{
		ID: id, OrgID: 7, Price: p, DiscountPercent: pct,
		SalePrice: common.DiscountedUnitPrice(p, pct), CostPrice: d("100.00"),
	}
}

// taxed sets the tax rate printed on a product: its shelf price already
// contains that tax.
func taxed(p *catalog.Product, rate string) *catalog.Product {
	p.TaxPercent = d(rate)
	return p
}

func (fx *saleFixture) hasProducts(ps ...*catalog.Product) {
	for _, p := range ps {
		fx.products.EXPECT().GetProduct(mock.Anything, uint(7), p.ID).Return(p, nil).Maybe()
		// no branch price override: the catalog price applies
		fx.products.EXPECT().GetBranchPrice(mock.Anything, uint(7), mock.Anything, p.ID).Return(nil, nil).Maybe()
	}
}

// hasBranchPrice stubs the product AND a branch override of it at branch 3
// (the fixture's selling branch); another branch has none.
func (fx *saleFixture) hasBranchPrice(p *catalog.Product, override *catalog.ProductBranchPrice) {
	fx.products.EXPECT().GetProduct(mock.Anything, uint(7), p.ID).Return(p, nil).Maybe()
	fx.products.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(3), p.ID).Return(override, nil).Maybe()
	fx.products.EXPECT().GetBranchPrice(mock.Anything, uint(7), mock.Anything, p.ID).Return(nil, nil).Maybe()
}

// persisted is what the service handed to the repository for one sale.
type persisted struct {
	sale   *Sale
	items  []SaleItem
	audits []*audit.AuditLog
	used   map[uuid.UUID]uuid.UUID // approval id -> sale id it was spent on
}

// expectPersist sets up the writes of a sale that is expected to succeed and
// records what was written. Stock is plentiful.
func (fx *saleFixture) expectPersist() *persisted {
	got := &persisted{used: map[uuid.UUID]uuid.UUID{}}
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, s *Sale) { got.sale = s }).Return(nil).Once()
	fx.repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, items []SaleItem) { got.items = items }).Return(nil).Once()
	fx.repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().MarkDiscountApprovalUsed(mock.Anything, mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, id uuid.UUID, saleID uuid.UUID) { got.used[id] = saleID }).Return(true, nil).Maybe()
	fx.inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, uint(3)).Return(&inventory.StockLevel{ID: 50, Qty: 100}, nil).Maybe()
	fx.inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	fx.inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { got.audits = append(got.audits, e) }).Return(nil).Maybe()
	return got
}

// expectNoWrites is for requests that must be refused before anything is
// written (no shift check, no insert, no approval spent).
func (fx *saleFixture) expectNothingWritten() {
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
}

func cashPayment(amount string) []CreateSalePaymentRequest {
	return []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: d(amount)}}
}

func oneSale(items []CreateSaleItemRequest, total string) CreateSaleRequest {
	return CreateSaleRequest{ID: uuid.New(), ShiftID: 9, DeviceID: 3, Items: items, Payments: cashPayment(total)}
}

func line(productID uint, qty int) CreateSaleItemRequest {
	return CreateSaleItemRequest{ProductID: productID, NameSnapshot: "Item", Qty: qty}
}

var seller = SaleActor{StaffID: 14}

func (fx *saleFixture) create(in CreateSaleRequest) (*Sale, error) {
	sale, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, in, false)
	return sale, err
}

// validApproval is an approval for 10% off product 1, covering 2 units, that
// seller 14 got approved by staff 17 on shift 9 of branch 3.
func validApproval() DiscountApproval {
	return DiscountApproval{
		ID: uuid.New(), OrgID: 7, BranchID: 3, ShiftID: 9, RequestedBy: 14, ApprovedBy: 17, ProductID: 1,
		Percent: d("10"), MaxQty: 2, Reason: "damaged box", Status: DiscountApprovalApproved,
		ExpiresAt: time.Now().Add(time.Hour),
	}
}

func (fx *saleFixture) hasApprovals(as ...DiscountApproval) {
	fx.repo.EXPECT().ListDiscountApprovals(mock.Anything, uint(7), mock.Anything).Return(as, nil).Maybe()
}

func withApproval(item CreateSaleItemRequest, a DiscountApproval) CreateSaleItemRequest {
	id := a.ID
	item.DiscountApprovalID = &id
	return item
}

// ---- pricing from the product ----

// A plain cashier - no discount permission of any kind - sells a product that
// carries a catalog discount. No approval, no permission: it's automatic.
func TestService_CreateSale_CatalogDiscountOnly_ByAPlainCashierNeedsNoApproval(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "10")) // sells at 5,900
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 2)}, "11800"))

	require.NoError(t, err)
	require.Len(t, got.items, 1)
	it := got.items[0]
	require.True(t, d("6500").Equal(it.UnitPrice), "unit_price is always the ORIGINAL price")
	require.True(t, d("10").Equal(it.CatalogDiscountPercent))
	require.True(t, d("1200").Equal(it.CatalogDiscount), "(6500 - 5900) x 2")
	require.True(t, it.ManualDiscount.IsZero())
	require.True(t, it.ComboDiscount.IsZero())
	require.True(t, d("1200").Equal(it.Discount), "discount = catalog + manual + combo")
	require.True(t, d("11800").Equal(it.LineTotal))
	require.Nil(t, it.DiscountApprovalID)
	require.True(t, d("13000").Equal(got.sale.Subtotal))
	require.True(t, d("1200").Equal(got.sale.Discount))
	require.True(t, d("11800").Equal(got.sale.Total))
	require.Empty(t, got.audits, "a catalog discount is not a privileged action")
}

func TestService_CreateSale_NoDiscountProduct_SellsAtItsPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "0"))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice", Qty: 2}}, "2000"))

	require.NoError(t, err)
	require.True(t, d("2000").Equal(got.sale.Subtotal))
	require.True(t, got.sale.Discount.IsZero())
	require.True(t, got.sale.Tax.IsZero(), "a 0%% product carries no tax")
	require.True(t, d("2000").Equal(got.sale.Total))
	require.True(t, d("100.00").Equal(got.items[0].UnitCost), "cost is snapshotted from the product, never the client")
}

// A product row that was never given a sale price (sale_price 0, no percent)
// must not suddenly sell for nothing.
func TestService_CreateSale_ProductWithoutAStoredSalePrice_SellsAtItsPrice(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(&catalog.Product{ID: 1, OrgID: 7, Price: d("1000")})
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "1000"))

	require.NoError(t, err)
	require.True(t, got.sale.Discount.IsZero())
}

// The client's own idea of the discount is not trusted on a plain line.
func TestService_CreateSale_ClientDiscountAmountOnAPlainLine_IsIgnored(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "0"))
	got := fx.expectPersist()
	item := line(1, 1)
	item.Discount = d("400")

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "1000"))

	require.NoError(t, err, "the total is still 1000: the client's 400 changed nothing")
	require.True(t, got.items[0].Discount.IsZero())
}

func TestService_CreateSale_PriceOverride_IsRejected(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	override := d("800")
	item := line(1, 1)
	item.PriceOverride = &override

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "800"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "price override is not allowed")
}

func TestService_CreateSale_ClientUnitPriceThatDiffers_IsConflict(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	item := line(1, 1)
	item.UnitPrice = d("6000") // the till's catalog is stale

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "6000"))

	requireRestErrorStatus(t, err, http.StatusConflict)
	require.Contains(t, err.Error(), "price changed, refresh the catalog")
}

func TestService_CreateSale_ClientUnitPriceThatMatches_IsFine(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	fx.expectPersist()
	item := line(1, 1)
	item.UnitPrice = d("6500")

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "6500"))

	require.NoError(t, err)
}

func TestService_CreateSale_PaymentsMismatch_RejectsWithoutOpeningTransaction(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "1000", "0"))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "500"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	fx.repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	fx.repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

// Paying the pre-discount price is still a mismatch: the total is what the
// server derived, discount and all.
func TestService_CreateSale_PayingTheOriginalPriceOfADiscountedLine_IsAMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6500", "10"))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "6500"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateSale_NegativeItemDiscount_Rejected(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	item := line(1, 1)
	item.Discount = d("-10")

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "1010"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateSale_UnknownProduct_PropagatesNotFound(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(nil, common.NotFoundError("product not found")).Once()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "1000"))

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// ---- combo lines: see sales_combo_test.go ----

func TestService_CreateSale_ApprovalOnAComboLine_IsBadRequest(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	a := validApproval()
	combo := uint(5)
	item := withApproval(CreateSaleItemRequest{ProductID: 1, ComboID: &combo, NameSnapshot: "Rice", Qty: 1}, a)

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "900"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// ---- custom (manual) discounts ----

// 10% off 6,500 is 5,850, rounded UP to 5,900. The approval comes off the
// original price and is stored on the line, which also carries who approved it.
func TestService_CreateSale_WithAValidApproval_StoresTheManualDiscountAndSpendsTheApproval(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	a := validApproval()
	fx.hasApprovals(a)
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900"))

	require.NoError(t, err)
	it := got.items[0]
	require.True(t, d("6500").Equal(it.UnitPrice))
	require.True(t, d("10").Equal(it.ManualDiscountPercent))
	require.True(t, d("600").Equal(it.ManualDiscount))
	require.Equal(t, "damaged box", it.ManualDiscountReason)
	require.True(t, it.CatalogDiscount.IsZero())
	require.True(t, it.CatalogDiscountPercent.IsZero(), "a manual % replaces the catalog %")
	require.True(t, d("600").Equal(it.Discount))
	require.True(t, d("5900").Equal(it.LineTotal))
	require.NotNil(t, it.DiscountApprovalID)
	require.Equal(t, a.ID, *it.DiscountApprovalID)
	require.NotNil(t, it.ManualApprovedBy)
	require.Equal(t, uint(17), *it.ManualApprovedBy)
	require.Equal(t, got.sale.ID, got.used[a.ID], "the approval is spent on this sale")
}

func TestService_CreateSale_WithAnApproval_WritesAManualDiscountAuditEntryPerApproval(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	a := validApproval()
	fx.hasApprovals(a)
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900"))

	require.NoError(t, err)
	require.Len(t, got.audits, 1)
	e := got.audits[0]
	require.Equal(t, "sale", e.Entity)
	require.Equal(t, "manual_discount", e.Action)
	require.Equal(t, got.sale.ID.String(), e.EntityID)
	require.Equal(t, uint(14), *e.ActorID)
	require.Contains(t, string(e.After), a.ID.String())
}

// The manual % REPLACES the catalog %, and may be smaller than it (the till
// only warns): the line is priced from the manual % alone.
func TestService_CreateSale_ManualPercentSmallerThanTheCatalogPercent_IsAllowedAndPricedFromTheManualPercent(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "20")) // catalog price 5,200
	a := validApproval()                            // 10% -> 5,900
	fx.hasApprovals(a)
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900"))

	require.NoError(t, err, "5,900 is dearer than the catalog's 5,200 and that's fine")
	require.True(t, d("600").Equal(got.items[0].ManualDiscount))
	require.True(t, got.items[0].CatalogDiscount.IsZero())
}

func TestService_CreateSale_ManualDiscount_UsesKyatRounding(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	a := validApproval()
	a.Percent = d("33") // 6,500 x 0.67 = 4,355 -> 4,400
	fx.hasApprovals(a)
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "4400"))

	require.NoError(t, err)
	require.True(t, d("2100").Equal(got.items[0].ManualDiscount))
}

func TestService_CreateSale_ApprovalCoversUpToMaxQty_ForALineOfThatSize(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	a := validApproval() // max 2
	fx.hasApprovals(a)
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 2), a)}, "11800"))

	require.NoError(t, err)
	require.True(t, d("1200").Equal(got.items[0].ManualDiscount), "600 x 2 units")
}

// The till can split one product into two lines - 1 of 2 coffees discounted.
func TestService_CreateSale_TwoLinesOfTheSameProduct_OneApprovedOneNot_AreEachPricedRight(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "10")) // catalog price 5,900
	a := validApproval()
	a.Percent = d("50") // manual: 3,250 -> 3,300
	fx.hasApprovals(a)
	got := fx.expectPersist()
	fx.inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, 98).Return(nil).Maybe()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a), line(1, 1)}, "9200")) // 3,300 + 5,900

	require.NoError(t, err)
	require.Len(t, got.items, 2)
	require.True(t, d("3200").Equal(got.items[0].ManualDiscount))
	require.True(t, got.items[0].CatalogDiscount.IsZero())
	require.True(t, d("600").Equal(got.items[1].CatalogDiscount))
	require.True(t, got.items[1].ManualDiscount.IsZero())
	require.Nil(t, got.items[1].DiscountApprovalID)
	require.True(t, d("3800").Equal(got.sale.Discount))
	require.Len(t, got.used, 1, "only the approved line spends an approval")
}

func TestService_CreateSale_TheSameApprovalOnTwoLines_IsConflict(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	a := validApproval()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a), withApproval(line(1, 1), a)}, "0"))

	requireRestErrorStatus(t, err, http.StatusConflict)
}

// Each way an approval can be unusable is a 409 with a message the till can show.
func TestService_CreateSale_UnusableApprovals_AreConflictsWithAClearMessage(t *testing.T) {
	cases := map[string]struct {
		mutate  func(a *DiscountApproval)
		qty     int
		product uint
		message string
	}{
		"already used":        {func(a *DiscountApproval) { a.Status = DiscountApprovalUsed }, 1, 1, "already used"},
		"cancelled":           {func(a *DiscountApproval) { a.Status = DiscountApprovalCancelled }, 1, 1, "cancelled"},
		"expired":             {func(a *DiscountApproval) { a.ExpiresAt = time.Now().Add(-time.Minute) }, 1, 1, "expired"},
		"another seller":      {func(a *DiscountApproval) { a.RequestedBy = 99 }, 1, 1, "different seller"},
		"another branch":      {func(a *DiscountApproval) { a.BranchID = 8 }, 1, 1, "not found for this branch"},
		"another shift":       {func(a *DiscountApproval) { a.ShiftID = 10 }, 1, 1, "different shift"},
		"more units than max": {func(a *DiscountApproval) {}, 3, 1, "covers 2 unit(s); the line has 3"},
		"another product":     {func(a *DiscountApproval) { a.ProductID = 2 }, 1, 1, "different product"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newSaleFixture(t)
			fx.expectNothingWritten()
			fx.hasProducts(catalogProduct(1, "6500", "0"), catalogProduct(2, "6500", "0"))
			a := validApproval()
			tc.mutate(&a)
			fx.hasApprovals(a)

			_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(tc.product, tc.qty), a)}, "0"))

			requireRestErrorStatus(t, err, http.StatusConflict)
			require.Contains(t, err.Error(), tc.message)
			fx.repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
			fx.repo.AssertNotCalled(t, "MarkDiscountApprovalUsed", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestService_CreateSale_ApprovalThatDoesNotExistOrBelongsToAnotherOrg_IsConflict(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	fx.hasApprovals() // the org-scoped lookup finds nothing
	a := validApproval()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "0"))

	requireRestErrorStatus(t, err, http.StatusConflict)
}

// Two sales race for one approval: both read it as approved, only one conditional
// update wins, and the loser's whole sale is rolled back.
func TestService_CreateSale_LosingTheRaceForAnApproval_IsConflict(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "0"))
	a := validApproval()
	fx.hasApprovals(a)
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	fx.inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, uint(3)).Return(&inventory.StockLevel{ID: 50, Qty: 100}, nil).Maybe()
	fx.inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	fx.inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	fx.repo.EXPECT().MarkDiscountApprovalUsed(mock.Anything, a.ID, mock.Anything).Return(false, nil).Once()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900"))

	requireRestErrorStatus(t, err, http.StatusConflict)
	require.Contains(t, err.Error(), "already used")
}

// The retry of a sale that already spent its approval must come back as the
// original, not trip over the now-used approval.
func TestService_CreateSale_ReplayAfterTheApprovalWasUsed_ReturnsTheOriginalSale(t *testing.T) {
	fx := newSaleFixture(t)
	a := validApproval()
	a.Status = DiscountApprovalUsed
	saleID := uuid.New()
	stored := &Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 3, StaffID: 14, Discount: d("600")}
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(stored, nil).Once()
	// No product lookup, approval lookup, shift check, insert, stock or audit.
	in := oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900")
	in.ID = saleID

	got, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, in, false)

	require.NoError(t, err)
	require.Same(t, stored, got)
	require.True(t, got.Replayed)
}

// ---- stock ----

func TestService_CreateSale_InsufficientStock_RollsBackWithConflict(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "0"))
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	fx.inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 70, ProductID: 1, BranchID: 3, Qty: 2}, nil).Once()
	// UpdateStockLevelQty/CreateInventoryLedgerEntry must never be called -
	// the movement is rejected before either write happens.

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 5)}, "5000"))

	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateSale_AllowNegativeStock_LetsItThroughAndReportsEvent(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "0"))
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	fx.inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 70, ProductID: 1, BranchID: 3, Qty: 2}, nil).Once()
	fx.inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(70), -3).Return(nil).Once()
	fx.inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
		return e.BalanceAfter == -3
	})).Return(nil).Once()

	_, events, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, oneSale([]CreateSaleItemRequest{line(1, 5)}, "5000"), true)

	require.NoError(t, err)
	require.Equal(t, []NegativeStockEvent{{ProductID: 1, BranchID: 3, ResultingQty: -3}}, events)
}

func TestService_CreateSale_SameProductAcrossMultipleLines_AggregatesQtyBeforeDecrementing(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "500", "0"))
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreateSaleItems(mock.Anything, mock.MatchedBy(func(items []SaleItem) bool {
		return len(items) == 2 && items[0].UnitCost.Equal(d("100.00")) && items[1].UnitCost.Equal(d("100.00"))
	})).Return(nil).Once()
	fx.repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	// One GetStockLevel/UpdateStockLevelQty pair, decrementing by the combined 5.
	fx.inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 71, ProductID: 1, BranchID: 3, Qty: 10}, nil).Once()
	fx.inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(71), 5).Return(nil).Once()
	fx.inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
		return e.ProductID == 1 && e.Qty == -5 && e.BalanceAfter == 5
	})).Return(nil).Once()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 2), line(1, 3)}, "2500"))

	require.NoError(t, err)
}

func TestService_CreateSale_ShiftNotOpen_PropagatesErrorWithoutPersisting(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "1000", "0"))
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).
		Return(common.NotFoundError("shift not found, not open, or doesn't belong to this branch")).Once()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "1000"))

	requireRestErrorStatus(t, err, http.StatusNotFound)
	fx.repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

// ---- idempotency ----

func replayRequest(saleID uuid.UUID) CreateSaleRequest {
	in := oneSale([]CreateSaleItemRequest{line(1, 1)}, "100")
	in.ID = saleID
	return in
}

func TestService_CreateSale_AlreadyStoredFromSameDevice_ReturnsOriginalWithoutWriting(t *testing.T) {
	fx := newSaleFixture(t)
	saleID := uuid.New()
	stored := &Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 3, StaffID: 14, Total: d("100")}
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(stored, nil).Once()
	// No product lookup, shift check, insert, stock or audit call: a retry is a pure read.

	got, negs, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, replayRequest(saleID), false)

	require.NoError(t, err)
	require.Empty(t, negs)
	require.Same(t, stored, got)
	require.True(t, got.Replayed)
}

func TestService_CreateSale_AlreadyStoredFromDifferentDevice_Conflicts(t *testing.T) {
	fx := newSaleFixture(t)
	saleID := uuid.New()
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 99}, nil).Once()

	_, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, replayRequest(saleID), false)

	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateSale_AlreadyStoredAtDifferentBranch_Conflicts(t *testing.T) {
	fx := newSaleFixture(t)
	saleID := uuid.New()
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, BranchID: 8, DeviceID: 3}, nil).Once()

	_, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, replayRequest(saleID), false)

	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateSale_LookupFails_PropagatesWithoutWriting(t *testing.T) {
	fx := newSaleFixture(t)
	boom := errors.New("db down")
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, boom).Once()

	_, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, replayRequest(uuid.New()), false)

	require.ErrorIs(t, err, boom)
}

// Two retries race past the lookup; the loser's insert hits the primary key.
// It must resolve to the winner's stored sale, not a 500.
func TestService_CreateSale_RaceLoser_ResolvesToStoredSale(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "100", "0"))
	saleID := uuid.New()
	stored := &Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 3}
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(nil, common.NotFoundError("sale not found")).Once()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(&pgconn.PgError{Code: "23505"}).Once()
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(stored, nil).Once()

	got, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, replayRequest(saleID), false)

	require.NoError(t, err)
	require.True(t, got.Replayed)
}

// The duplicate key belongs to another org's sale: this org can't see it, and
// must not be handed it or told more than "already exists".
func TestService_CreateSale_RaceLoser_IdOwnedByAnotherOrg_Conflicts(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "100", "0"))
	saleID := uuid.New()
	fx.repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(nil, common.NotFoundError("sale not found")).Twice()
	fx.repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	fx.repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(&pgconn.PgError{Code: "23505"}).Once()

	_, _, err := fx.svc.CreateSale(context.Background(), 7, 3, seller, replayRequest(saleID), false)

	requireRestErrorStatus(t, err, http.StatusConflict)
}

// ---- offline-queued sales (priced from the till's cached catalog) ----

func (fx *saleFixture) createQueued(in CreateSaleRequest) (*Sale, []NegativeStockEvent, error) {
	return fx.svc.CreateSale(context.Background(), 7, 3, seller, in, true)
}

// A queued sale takes the till's cached unit price and catalog %, and
// recomputes the line from them with the Kyat rounding rule.
func TestService_CreateSale_Queued_PricesTheLineFromTheTillsCachedPriceAndPercent(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "10")) // current: matches the till
	got := fx.expectPersist()
	pct := d("10")
	item := line(1, 1)
	item.UnitPrice = d("6500")
	item.CatalogDiscountPercent = &pct

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "5900"))

	require.NoError(t, err)
	require.True(t, d("600").Equal(got.items[0].CatalogDiscount))
	require.True(t, d("10").Equal(got.items[0].CatalogDiscountPercent))
	require.Empty(t, sale.PriceMismatches, "nothing is stale")
	require.NotNil(t, got.sale.SyncedAt)
}

// The till's cache was stale: it sold at 6,000 with no discount, the product is
// now 6,500 at 10%. The sale stands as rung up, and the line is reported.
func TestService_CreateSale_Queued_StalePrice_IsAcceptedAsRungUpAndReportedAsAMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "10"))
	got := fx.expectPersist()
	item := line(1, 1)
	item.UnitPrice = d("6000")

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "6000"))

	require.NoError(t, err)
	require.True(t, d("6000").Equal(got.items[0].UnitPrice), "the till's price, not today's")
	require.Len(t, sale.PriceMismatches, 1)
	m := sale.PriceMismatches[0]
	require.Equal(t, uint(1), m.ProductID)
	require.True(t, d("6000").Equal(m.ClientUnitPrice))
	require.True(t, d("6500").Equal(m.CurrentUnitPrice))
	require.True(t, d("10").Equal(m.CurrentPercent))
}

func TestService_CreateSale_Queued_StalePercentAlone_IsAlsoAMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "10"))
	fx.expectPersist()
	pct := d("5") // the till still had the old 5%
	item := line(1, 1)
	item.UnitPrice = d("6500")
	item.CatalogDiscountPercent = &pct

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "6200")) // 6,500 less 5% = 6,175 -> 6,200

	require.NoError(t, err)
	require.Len(t, sale.PriceMismatches, 1)
}

func TestService_CreateSale_Queued_PercentOutOfRange_IsBadRequest(t *testing.T) {
	for name, pct := range map[string]string{"negative": "-1", "over 100": "100.5"} {
		t.Run(name, func(t *testing.T) {
			fx := newSaleFixture(t)
			fx.expectNothingWritten()
			fx.hasProducts(catalogProduct(1, "6500", "0"))
			value := d(pct)
			item := line(1, 1)
			item.UnitPrice = d("6500")
			item.CatalogDiscountPercent = &value

			_, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "0"))

			requireRestErrorStatus(t, err, http.StatusBadRequest)
		})
	}
}

// Offline, the till taxed the line from the rate in its cached catalog, and
// that rate is used - the customer already paid what the till showed.
func TestService_CreateSale_Queued_UsesTheTillsCachedTaxRate(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5")) // the rate has not changed
	got := fx.expectPersist()
	rate := d("5")
	item := line(1, 1)
	item.UnitPrice = d("10500")
	item.TaxPercent = &rate

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "10500"))

	require.NoError(t, err)
	require.True(t, d("500").Equal(got.items[0].Tax))
	require.True(t, d("5").Equal(got.items[0].TaxPercent))
	require.True(t, d("10500").Equal(got.sale.Total), "tax is inside the price offline too")
	require.Empty(t, sale.PriceMismatches, "nothing is stale")
}

// The Owner changed the rate after the till cached it: the sale keeps the
// till's 0%, and the SAME price_mismatch report (not a new kind of conflict)
// carries both rates. It is never rejected for it.
func TestService_CreateSale_Queued_StaleTaxRate_IsAcceptedAndReportedAsAMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5")) // now 5%
	got := fx.expectPersist()
	rate := d("0") // the till still had 0%
	item := line(1, 1)
	item.UnitPrice = d("10500")
	item.TaxPercent = &rate

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "10500"))

	require.NoError(t, err)
	require.True(t, got.items[0].Tax.IsZero(), "taxed at the till's rate")
	require.Len(t, sale.PriceMismatches, 1)
	m := sale.PriceMismatches[0]
	require.True(t, d("0").Equal(m.ClientTaxPercent))
	require.True(t, d("5").Equal(m.CurrentTaxPercent))
	require.True(t, d("10500").Equal(m.ClientUnitPrice), "the price itself was not stale")
}

// A till that predates tax rates sends none: the product's current rate is used
// (and, with nothing claimed, nothing can be stale).
func TestService_CreateSale_Queued_NoTaxRateSent_UsesTheProductsCurrentRate(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5"))
	got := fx.expectPersist()
	item := line(1, 1)
	item.UnitPrice = d("10500")

	sale, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "10500"))

	require.NoError(t, err)
	require.True(t, d("500").Equal(got.items[0].Tax))
	require.Empty(t, sale.PriceMismatches)
}

func TestService_CreateSale_Queued_TaxRateOutOfRange_IsBadRequest(t *testing.T) {
	for name, pct := range map[string]string{"negative": "-1", "over 100": "100.5"} {
		t.Run(name, func(t *testing.T) {
			fx := newSaleFixture(t)
			fx.expectNothingWritten()
			fx.hasProducts(catalogProduct(1, "6500", "0"))
			value := d(pct)
			item := line(1, 1)
			item.UnitPrice = d("6500")
			item.TaxPercent = &value

			_, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{item}, "6500"))

			requireRestErrorStatus(t, err, http.StatusBadRequest)
		})
	}
}

// A live sale ignores a tax_percent the till sends: only a queued sale is taxed
// from the till's snapshot.
func TestService_CreateSale_Live_IgnoresTheTillsTaxPercentSnapshot(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5"))
	got := fx.expectPersist()
	rate := d("0")
	item := line(1, 1)
	item.TaxPercent = &rate

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "10500"))

	require.NoError(t, err)
	require.True(t, d("500").Equal(got.items[0].Tax))
}

// There is no approval offline: a PIN can't be checked.
func TestService_CreateSale_Queued_ApprovalIdIsRejected(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	a := validApproval()

	_, _, err := fx.createQueued(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900"))

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	require.Contains(t, err.Error(), "can't be queued offline")
}

// A live sale ignores a catalog_discount_percent the till sends: only a queued
// sale is priced from the till's snapshot.
func TestService_CreateSale_Live_IgnoresTheTillsCatalogPercentSnapshot(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "6500", "10"))
	got := fx.expectPersist()
	pct := d("50")
	item := line(1, 1)
	item.CatalogDiscountPercent = &pct

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "5900"))

	require.NoError(t, err)
	require.True(t, d("10").Equal(got.items[0].CatalogDiscountPercent), "the product's own 10%%")
}

// ---- tax: inside the price, worked out by the server ----

// The shelf price already contains the tax, so the customer pays exactly the
// price and the tax is just the part of it that is tax: K 10,500 at 5% -> K 500.
func TestService_CreateSale_FivePercentLine_TotalIsThePriceAndTaxIsTheIncludedPart(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5"))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "10500"))

	require.NoError(t, err)
	require.True(t, d("10500").Equal(got.sale.Total), "tax is never added on top")
	require.True(t, d("500").Equal(got.sale.Tax))
	require.True(t, d("500").Equal(got.items[0].Tax))
	require.True(t, d("5").Equal(got.items[0].TaxPercent), "the rate is stored on the line, so a later return doesn't depend on today's product")
	require.True(t, d("10500").Equal(got.items[0].LineTotal))
}

// The old till paid price + tax. That is now an overpayment and is refused.
func TestService_CreateSale_PayingPricePlusTax_IsAMismatch(t *testing.T) {
	fx := newSaleFixture(t)
	fx.expectNothingWritten()
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5"))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "11000")) // 10,500 + 500

	requireRestErrorStatus(t, err, http.StatusBadRequest)
	fx.repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_ZeroPercentProduct_HasNoTax(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(catalogProduct(1, "10500", "0"))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1)}, "10500"))

	require.NoError(t, err)
	require.True(t, got.sale.Tax.IsZero())
	require.True(t, got.items[0].Tax.IsZero())
	require.True(t, got.items[0].TaxPercent.IsZero())
}

// A discount lowers what is paid, so it lowers the tax inside it: the tax is
// taken from the line total AFTER the catalog discount.
func TestService_CreateSale_CatalogDiscount_LowersTheTax(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "6500", "10"), "5")) // sells at 5,900
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 2)}, "11800"))

	require.NoError(t, err)
	require.True(t, d("11800").Equal(got.sale.Total))
	require.True(t, d("561.90").Equal(got.items[0].Tax), "11,800 x 5 / 105, not 13,000 x 5 / 105 (got %s)", got.items[0].Tax)
	require.True(t, d("561.90").Equal(got.sale.Tax))
}

// Same for a custom discount: the approval's % replaces the catalog %, and the
// tax follows the price actually paid.
func TestService_CreateSale_CustomDiscount_LowersTheTax(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "6500", "0"), "5"))
	got := fx.expectPersist()
	a := validApproval() // 10% off product 1, up to 2 units
	fx.hasApprovals(a)

	_, err := fx.create(oneSale([]CreateSaleItemRequest{withApproval(line(1, 1), a)}, "5900"))

	require.NoError(t, err)
	require.True(t, d("5900").Equal(got.sale.Total))
	require.True(t, d("280.95").Equal(got.items[0].Tax), "5,900 x 5 / 105 (got %s)", got.items[0].Tax)
	require.True(t, d("5").Equal(got.items[0].TaxPercent))
}

// (A combo line is taxed after its bundle share: see sales_combo_test.go.)

// Each line uses its own product's rate; the sale's tax is the sum of the
// per-line (already rounded) taxes.
func TestService_CreateSale_LinesWithDifferentRates_EachUseTheirOwn(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5"), taxed(catalogProduct(2, "1100", "0"), "10"), catalogProduct(3, "700", "0"))
	got := fx.expectPersist()

	_, err := fx.create(oneSale([]CreateSaleItemRequest{line(1, 1), line(2, 1), line(3, 1)}, "12300"))

	require.NoError(t, err)
	require.True(t, d("500").Equal(got.items[0].Tax))
	require.True(t, d("100").Equal(got.items[1].Tax))
	require.True(t, got.items[2].Tax.IsZero())
	require.True(t, d("600").Equal(got.sale.Tax))
	require.True(t, d("12300").Equal(got.sale.Total))
}

// The client no longer says what the tax is: a `tax` field on a line (an old
// till still sends one) is simply not read.
func TestService_CreateSale_ClientTaxOnALine_IsIgnored(t *testing.T) {
	fx := newSaleFixture(t)
	fx.hasProducts(taxed(catalogProduct(1, "10500", "0"), "5"))
	got := fx.expectPersist()
	var item CreateSaleItemRequest
	require.NoError(t, json.Unmarshal([]byte(`{"product_id":1,"name_snapshot":"Rice","qty":1,"tax":"9999"}`), &item))

	_, err := fx.create(oneSale([]CreateSaleItemRequest{item}, "10500"))

	require.NoError(t, err)
	require.True(t, d("500").Equal(got.items[0].Tax), "the server's 500, not the client's 9,999")
	require.True(t, d("10500").Equal(got.sale.Total))
}
