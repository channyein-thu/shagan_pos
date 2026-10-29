package datasync

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/sales"
	"shagan_pos/internal/subscription"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - same reasoning as every other domain's own
// equivalent this session.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

// fakeEntitlements answers every org with the same fixed entitlements.
type fakeEntitlements struct{ ent subscription.Entitlements }

func (f fakeEntitlements) Entitlements(context.Context, uint, time.Time) (*subscription.Entitlements, error) {
	ent := f.ent
	return &ent, nil
}

var basicEntitlements = fakeEntitlements{subscription.Entitlements{Plan: subscription.PlanBasic, Features: []string{}}}

func newTestService(repo Repository, branches BranchLookup, catalogReader CatalogReader, salesWriter SalesWriter) *Service {
	// No staff lookup: only IngestQueuedSales needs one - see newIngestService.
	return NewService(repo, branches, nil, catalogReader, basicEntitlements, salesWriter, fakeTransactioner{})
}

func newIngestService(repo Repository, staff StaffLookup, salesWriter SalesWriter) *Service {
	return NewService(repo, nil, staff, nil, basicEntitlements, salesWriter, fakeTransactioner{})
}

// --- GetCatalogSnapshot ---

func TestService_GetCatalogSnapshot_ComposesAllThreeAndComputesETag(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	products := []catalog.ProductResult{{Product: catalog.Product{ID: 1, Name: "Cola"}}}
	categories := []catalog.Category{{ID: 1, NameI18n: "Beverages"}}
	combos := []catalog.ComboResult{{Combo: catalog.Combo{ID: 1}}}

	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(products, nil).Once()
	cat.EXPECT().ListCategories(mock.Anything, uint(7)).Return(categories, nil).Once()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(combos, nil).Once()

	got, etag, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, products, got.Products)
	require.Equal(t, categories, got.Categories)
	require.Equal(t, combos, got.Combos)
	require.NotEmpty(t, etag)
}

func TestService_GetCatalogSnapshot_SameContentYieldsSameETag(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	products := []catalog.ProductResult{{Product: catalog.Product{ID: 1, Name: "Cola"}}}
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(products, nil).Twice()
	cat.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, nil).Twice()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(nil, nil).Twice()

	_, etag1, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	_, etag2, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, etag1, etag2)
}

// A combo's component items are part of what a till caches, so changing them
// must change the ETag - otherwise a device would keep expanding a combo into
// its old products after a 304.
func TestService_GetCatalogSnapshot_ComboItemsChange_YieldsDifferentETag(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	before := []catalog.ComboResult{{Combo: catalog.Combo{ID: 1, Name: "Breakfast"}, Items: []catalog.ComboItemResult{{ProductID: 5, Qty: 1}}}}
	after := []catalog.ComboResult{{Combo: catalog.Combo{ID: 1, Name: "Breakfast"}, Items: []catalog.ComboItemResult{{ProductID: 5, Qty: 2}}}}
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(nil, nil).Twice()
	cat.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, nil).Twice()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(before, nil).Once()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(after, nil).Once()

	snap, etag1, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, before[0].Items, snap.Combos[0].Items, "the snapshot must carry the items through")
	_, etag2, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	require.NotEqual(t, etag1, etag2)
}

// A product's tax rate is part of what a till caches (it prices and taxes
// offline from it), so a rate change must change the ETag - otherwise a till
// would keep taxing at the old rate after a 304.
func TestService_GetCatalogSnapshot_TaxPercentChange_YieldsDifferentETag(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	rate := func(p string) []catalog.ProductResult {
		return []catalog.ProductResult{{Product: catalog.Product{ID: 1, Name: "Cola", TaxPercent: decimal.RequireFromString(p)}}}
	}
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(rate("0"), nil).Once()
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(rate("5"), nil).Once()
	cat.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, nil).Twice()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(nil, nil).Twice()

	_, etag1, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	snap, etag2, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	require.NotEqual(t, etag1, etag2)
	require.True(t, decimal.NewFromInt(5).Equal(snap.Products[0].TaxPercent), "the snapshot must carry the rate")
}

// TestService_GetCatalogSnapshot_DifferingOnlyByPresignedImageURL_YieldsSameETag
// guards against a real bug caught in live testing: a presigned image URL
// is freshly re-signed (different timestamp/signature) on every single
// call regardless of whether the image itself changed - hashing it
// directly would mean the ETag never matches twice, defeating
// If-None-Match caching entirely.
func TestService_GetCatalogSnapshot_DifferingOnlyByPresignedImageURL_YieldsSameETag(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	productWithImage := func(url string) []catalog.ProductResult {
		return []catalog.ProductResult{{
			Product: catalog.Product{ID: 1, Name: "Cola"},
			Images:  []catalog.ProductImageResult{{ID: 10, URL: url, Width: 100, Height: 100}},
		}}
	}
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(productWithImage("https://cdn.example/a?sig=111"), nil).Once()
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(productWithImage("https://cdn.example/a?sig=222"), nil).Once()
	cat.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, nil).Twice()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(nil, nil).Twice()

	_, etag1, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	_, etag2, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, etag1, etag2)
}

// --- IngestQueuedSales ---

// A queued sale from branch 5, staff 14 (a member of branch 5), device 3.
func queuedSale(id uuid.UUID) sales.CreateSaleRequest {
	return sales.CreateSaleRequest{
		ID: id, StaffID: 14, ShiftID: 9, DeviceID: 3,
		Items: []sales.CreateSaleItemRequest{{ProductID: 1, Qty: 1, Discount: decimal.Zero}},
	}
}

func notStored(sw *MockSalesWriter, id uuid.UUID) {
	sw.EXPECT().GetSale(mock.Anything, uint(7), id).Return(nil, common.NotFoundError("sale not found")).Once()
}

func staffOfBranch(staff *MockStaffLookup, id, branchID uint) {
	staff.EXPECT().GetStaff(mock.Anything, uint(7), id).Return(&identity.Staff{ID: id, BranchID: branchID}, nil).Once()
}

func TestService_IngestQueuedSales_NewSale_Created(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().
		CreateSale(mock.Anything, uint(7), uint(5), sales.SaleActor{StaffID: 14}, req, true).
		Return(&sales.Sale{ID: saleID}, nil, nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.Len(t, got.Results, 1)
	require.True(t, got.Results[0].Success)
	require.Empty(t, got.Results[0].Error)
	require.Equal(t, saleID.String(), got.Results[0].SaleID)
}

// Catalog discounts are automatic - no approval - so they work offline too (R6).
// The till sends its cached unit price and catalog %, which the sale is priced from.
func TestService_IngestQueuedSales_CatalogDiscount_IsAccepted(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	pct := decimal.NewFromInt(10)
	req.Items[0].UnitPrice = decimal.NewFromInt(6500)
	req.Items[0].CatalogDiscountPercent = &pct

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), sales.SaleActor{StaffID: 14}, req, true).
		Return(&sales.Sale{ID: saleID}, nil, nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})

	require.NoError(t, err)
	require.True(t, got.Results[0].Success, got.Results[0].Error)
}

// There is no approval offline: a PIN can't be checked, so a line carrying one
// is refused before anything is created.
func TestService_IngestQueuedSales_DiscountApprovalId_IsRejectedWithoutCreating(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	approval := uuid.New()
	req.Items[0].DiscountApprovalID = &approval

	notStored(sw, saleID)
	// Neither GetStaff nor CreateSale is reached.

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})

	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
	require.Contains(t, got.Results[0].Error, "can't be queued offline")
}

// The till priced the sale from a cached catalog that may be stale: the sale is
// accepted as rung up, and each stale line is recorded as a conflict for a
// manager to look at - not rejected.
func TestService_IngestQueuedSales_StalePrice_IsAcceptedAndRecordedAsAConflict(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	mismatch := sales.PriceMismatch{
		ProductID: 1, ClientUnitPrice: decimal.NewFromInt(6000), ClientPercent: decimal.Zero,
		CurrentUnitPrice: decimal.NewFromInt(6500), CurrentPercent: decimal.NewFromInt(10),
	}

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req, true).
		Return(&sales.Sale{ID: saleID, PriceMismatches: []sales.PriceMismatch{mismatch}}, nil, nil).Once()
	repo.EXPECT().
		CreateSyncConflict(mock.Anything, mock.MatchedBy(func(c *SyncConflict) bool {
			return c.SaleID == saleID && c.Reason == SyncConflictReasonPriceMismatch &&
				strings.Contains(string(c.Payload), `"product_id":1`) && strings.Contains(string(c.Payload), `"client_unit_price":"6000"`)
		})).
		Return(nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})

	require.NoError(t, err)
	require.True(t, got.Results[0].Success, "the sale is accepted, not rejected")
}

// A stale tax rate rides on the same price_mismatch conflict as a stale price
// or discount: one conflict per line, carrying both rates.
func TestService_IngestQueuedSales_StaleTaxRate_IsRecordedInThePriceMismatchConflict(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	mismatch := sales.PriceMismatch{
		ProductID: 1, ClientUnitPrice: decimal.NewFromInt(10500), CurrentUnitPrice: decimal.NewFromInt(10500),
		ClientTaxPercent: decimal.Zero, CurrentTaxPercent: decimal.NewFromInt(5),
	}

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req, true).
		Return(&sales.Sale{ID: saleID, PriceMismatches: []sales.PriceMismatch{mismatch}}, nil, nil).Once()
	repo.EXPECT().
		CreateSyncConflict(mock.Anything, mock.MatchedBy(func(c *SyncConflict) bool {
			return c.SaleID == saleID && c.Reason == SyncConflictReasonPriceMismatch &&
				strings.Contains(string(c.Payload), `"client_tax_percent":"0"`) && strings.Contains(string(c.Payload), `"current_tax_percent":"5"`)
		})).
		Return(nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})

	require.NoError(t, err)
	require.True(t, got.Results[0].Success, "the sale is accepted, not rejected")
}

// A combo that changed price while the till was offline is reported once per
// combo on the same price_mismatch conflict: what the till charged for the
// bundle against what the server now charges. The sale is accepted.
func TestService_IngestQueuedSales_ComboThatChangedPrice_IsRecordedInThePriceMismatchConflict(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	comboID, charged, now := uint(5), decimal.NewFromInt(9500), decimal.NewFromInt(9000)
	mismatch := sales.PriceMismatch{ComboID: &comboID, ClientBundleTotal: &charged, CurrentBundleTotal: &now}

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req, true).
		Return(&sales.Sale{ID: saleID, PriceMismatches: []sales.PriceMismatch{mismatch}}, nil, nil).Once()
	repo.EXPECT().
		CreateSyncConflict(mock.Anything, mock.MatchedBy(func(c *SyncConflict) bool {
			return c.SaleID == saleID && c.Reason == SyncConflictReasonPriceMismatch &&
				strings.Contains(string(c.Payload), `"combo_id":5`) &&
				strings.Contains(string(c.Payload), `"client_bundle_total":"9500"`) &&
				strings.Contains(string(c.Payload), `"current_bundle_total":"9000"`)
		})).
		Return(nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})

	require.NoError(t, err)
	require.True(t, got.Results[0].Success, "the sale is accepted, not rejected")
}

// A fake combo is refused for that sale only; the next queued sale still goes in.
func TestService_IngestQueuedSales_FakeCombo_FailsThatSaleAndTheRestContinue(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	badID, goodID := uuid.New(), uuid.New()
	bad, good := queuedSale(badID), queuedSale(goodID)

	notStored(sw, badID)
	notStored(sw, goodID)
	staffOfBranch(staff, 14, 5)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, bad, true).
		Return(nil, nil, common.BadRequestError("combo 99999 does not exist")).Once()
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, good, true).
		Return(&sales.Sale{ID: goodID}, nil, nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{bad, good}})

	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
	require.Contains(t, got.Results[0].Error, "combo 99999 does not exist")
	require.True(t, got.Results[1].Success)
}

func TestService_IngestQueuedSales_StaffFromAnotherBranch_FailsWithoutCreating(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 6) // device is branch 5

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
	require.Contains(t, got.Results[0].Error, "staff_id")
}

func TestService_IngestQueuedSales_StaffNotFoundInOrg_FailsWithoutCreating(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	notStored(sw, saleID)
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(14)).Return(nil, common.NotFoundError("staff not found")).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
	require.Contains(t, got.Results[0].Error, "staff_id")
}

func TestService_IngestQueuedSales_MissingStaffID_FailsWithoutLookup(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	req.StaffID = 0

	notStored(sw, saleID)

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
}

func TestService_IngestQueuedSales_StaffLookupInfraError_FailsItemWithoutCreating(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	notStored(sw, saleID)
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(14)).Return(nil, errors.New("db down")).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
}

func TestService_IngestQueuedSales_AlreadyStoredFromSameDevice_IdempotentSuccessEvenIfStaffGone(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t) // never consulted: a retry must not fail on later staff changes
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	sw.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&sales.Sale{ID: saleID, BranchID: 5, DeviceID: 3}, nil).Once()
	// CreateSale must never be called - already arrived, a safe no-op.

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.True(t, got.Results[0].Success)
}

func TestService_IngestQueuedSales_IdStoredFromAnotherDevice_FailsInsteadOfSilentSuccess(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	sw.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&sales.Sale{ID: saleID, BranchID: 5, DeviceID: 99}, nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.False(t, got.Results[0].Success, "dropping a different till's sale as 'already synced' would lose it")
	require.Contains(t, got.Results[0].Error, "already exists")
}

func TestService_IngestQueuedSales_CreateSaleFails_ReportedAsFailedItemNotError(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req, true).
		Return(nil, nil, common.NotFoundError("shift not found, not open, or doesn't belong to this branch")).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.False(t, got.Results[0].Success)
	require.NotEmpty(t, got.Results[0].Error)
}

func TestService_IngestQueuedSales_NegativeStockEvent_WritesSyncConflict(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID := uuid.New()
	req := queuedSale(saleID)
	events := []sales.NegativeStockEvent{{ProductID: 2, BranchID: 5, ResultingQty: -1}}

	notStored(sw, saleID)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req, true).
		Return(&sales.Sale{ID: saleID}, events, nil).Once()
	repo.EXPECT().
		CreateSyncConflict(mock.Anything, mock.MatchedBy(func(c *SyncConflict) bool {
			return c.SaleID == saleID && c.Reason == SyncConflictReasonStockConflict
		})).
		Return(nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req}})
	require.NoError(t, err)
	require.True(t, got.Results[0].Success)
}

func TestService_IngestQueuedSales_MultipleItems_ProcessedIndependently(t *testing.T) {
	repo := NewMockRepository(t)
	staff := NewMockStaffLookup(t)
	sw := NewMockSalesWriter(t)
	svc := newIngestService(repo, staff, sw)

	saleID1 := uuid.New()
	saleID2 := uuid.New()
	req1 := queuedSale(saleID1)
	req2 := queuedSale(saleID2)

	notStored(sw, saleID1)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req1, true).
		Return(nil, nil, common.BadRequestError("payments must add up to the sale total")).Once()
	notStored(sw, saleID2)
	staffOfBranch(staff, 14, 5)
	sw.EXPECT().CreateSale(mock.Anything, uint(7), uint(5), mock.Anything, req2, true).
		Return(&sales.Sale{ID: saleID2}, nil, nil).Once()

	got, err := svc.IngestQueuedSales(context.Background(), 7, 5, IngestQueuedSalesRequest{Sales: []sales.CreateSaleRequest{req1, req2}})
	require.NoError(t, err)
	require.Len(t, got.Results, 2)
	require.False(t, got.Results[0].Success)
	require.True(t, got.Results[1].Success)
}

// --- GetSyncStatus / ListSyncConflicts ---

func TestService_GetSyncStatus_OrgWide_ComposesCountAndLastSyncedAt(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	lastSynced := time.Now()
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()
	repo.EXPECT().UnresolvedConflictCount(mock.Anything, []uint{5, 6}).Return(int64(2), nil).Once()
	repo.EXPECT().LastSyncedAt(mock.Anything, []uint{5, 6}).Return(&lastSynced, nil).Once()

	got, err := svc.GetSyncStatus(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.UnresolvedConflictCount)
	require.Equal(t, &lastSynced, got.LastSyncedAt)
}

func TestService_ListSyncConflicts_ScopedToOneBranch(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	branchID := uint(5)
	want := []SyncConflict{{ID: 1, SaleID: uuid.New()}}
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListSyncConflicts(mock.Anything, []uint{5}).Return(want, nil).Once()

	got, err := svc.ListSyncConflicts(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// --- ResolveSyncConflict ---

func TestService_ResolveSyncConflict_HappyPath(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	conflict := &SyncConflict{ID: 1, SaleID: uuid.New()}
	repo.EXPECT().GetSyncConflictWithLock(mock.Anything, []uint{5}, uint(1)).Return(conflict, nil).Once()
	repo.EXPECT().UpdateSyncConflictResolved(mock.Anything, uint(1), uint(9), mock.Anything).Return(nil).Once()

	got, err := svc.ResolveSyncConflict(context.Background(), 7, 9, 1)
	require.NoError(t, err)
	require.NotNil(t, got.ResolvedBy)
	require.Equal(t, uint(9), *got.ResolvedBy)
	require.NotNil(t, got.ResolvedAt)
}

func TestService_ResolveSyncConflict_AlreadyResolved_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	resolvedAt := time.Now()
	resolvedBy := uint(3)
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	conflict := &SyncConflict{ID: 1, ResolvedBy: &resolvedBy, ResolvedAt: &resolvedAt}
	repo.EXPECT().GetSyncConflictWithLock(mock.Anything, []uint{5}, uint(1)).Return(conflict, nil).Once()
	// UpdateSyncConflictResolved must never be called.

	_, err := svc.ResolveSyncConflict(context.Background(), 7, 9, 1)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_ResolveSyncConflict_NotFound_PropagatesError(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	cat := NewMockCatalogReader(t)
	sw := NewMockSalesWriter(t)
	svc := newTestService(repo, branches, cat, sw)

	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	repo.EXPECT().GetSyncConflictWithLock(mock.Anything, []uint{5}, uint(99)).
		Return(nil, common.NotFoundError("sync conflict not found")).Once()

	_, err := svc.ResolveSyncConflict(context.Background(), 7, 9, 99)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// --- branch prices in the snapshot ---

func catalogForBranch(t *testing.T) (*Service, *MockCatalogReader) {
	t.Helper()
	cat := NewMockCatalogReader(t)
	cat.EXPECT().ListCategories(mock.Anything, uint(7)).Return(nil, nil).Maybe()
	cat.EXPECT().ListCombos(mock.Anything, uint(7)).Return(nil, nil).Maybe()
	return newTestService(NewMockRepository(t), NewMockBranchLookup(t), cat, NewMockSalesWriter(t)), cat
}

func productAt(price string) []catalog.ProductResult {
	p := decimal.RequireFromString(price)
	return []catalog.ProductResult{{Product: catalog.Product{ID: 1, Name: "Cola", Price: p, SalePrice: p}, PriceSource: catalog.PriceSourceBranch}}
}

// Two branches that price the same product differently get different prices
// AND different ETags - a till must never keep another branch's cached prices
// on a 304.
func TestService_GetCatalogSnapshot_DifferentBranches_GetTheirOwnPricesAndETags(t *testing.T) {
	svc, cat := catalogForBranch(t)
	b3, b4 := uint(3), uint(4)
	cat.EXPECT().ListProducts(mock.Anything, uint(7), &b3).Return(productAt("7000"), nil).Once()
	cat.EXPECT().ListProducts(mock.Anything, uint(7), &b4).Return(productAt("6500"), nil).Once()

	snap3, etag3, err := svc.GetCatalogSnapshot(context.Background(), 7, &b3)
	require.NoError(t, err)
	snap4, etag4, err := svc.GetCatalogSnapshot(context.Background(), 7, &b4)
	require.NoError(t, err)

	require.True(t, decimal.RequireFromString("7000").Equal(snap3.Products[0].Price))
	require.True(t, decimal.RequireFromString("6500").Equal(snap4.Products[0].Price))
	require.NotEqual(t, etag3, etag4)
}

// The branch is part of the ETag itself: two branches whose content happens to
// be identical today still don't share one.
func TestService_GetCatalogSnapshot_SameContentAtTwoBranches_YieldsDifferentETags(t *testing.T) {
	svc, cat := catalogForBranch(t)
	b3, b4 := uint(3), uint(4)
	cat.EXPECT().ListProducts(mock.Anything, uint(7), mock.Anything).Return(productAt("6500"), nil).Times(3)

	_, etag3, err := svc.GetCatalogSnapshot(context.Background(), 7, &b3)
	require.NoError(t, err)
	_, etag4, err := svc.GetCatalogSnapshot(context.Background(), 7, &b4)
	require.NoError(t, err)
	_, etag3Again, err := svc.GetCatalogSnapshot(context.Background(), 7, &b3)
	require.NoError(t, err)

	require.NotEqual(t, etag3, etag4)
	require.Equal(t, etag3, etag3Again, "still stable for one branch")
}

// --- the plan and features in the snapshot ---

func TestService_GetCatalogSnapshot_CarriesThePlanAndFeatures(t *testing.T) {
	svc, cat := catalogForBranch(t)
	svc.subs = fakeEntitlements{subscription.Entitlements{Plan: "pro", Features: []string{"customer_display", "promotions"}}}
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(nil, nil).Once()

	snap, _, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)

	require.NoError(t, err)
	require.Equal(t, "pro", snap.Plan)
	require.Equal(t, []string{"customer_display", "promotions"}, snap.Features)
}

// A plan (or feature-override) change must give the till a fresh snapshot, not a 304.
func TestService_GetCatalogSnapshot_ThePlanIsPartOfTheETag(t *testing.T) {
	svc, cat := catalogForBranch(t)
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(nil, nil).Times(4)
	snapshotETag := func(ent subscription.Entitlements) string {
		svc.subs = fakeEntitlements{ent}
		_, etag, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)
		require.NoError(t, err)
		return etag
	}

	basic := snapshotETag(subscription.Entitlements{Plan: "basic", Features: []string{}})
	basicAgain := snapshotETag(subscription.Entitlements{Plan: "basic", Features: []string{}})
	pro := snapshotETag(subscription.Entitlements{Plan: "pro", Features: []string{"customer_display"}})
	// same plan, but one feature switched on for this org
	basicWithBeta := snapshotETag(subscription.Entitlements{Plan: "basic", Features: []string{"loyalty"}})

	require.Equal(t, basic, basicAgain)
	require.NotEqual(t, basic, pro)
	require.NotEqual(t, basic, basicWithBeta)
}

func TestService_GetCatalogSnapshot_AnEntitlementsFailureIsPropagated(t *testing.T) {
	svc, cat := catalogForBranch(t)
	svc.subs = failingEntitlements{}
	cat.EXPECT().ListProducts(mock.Anything, uint(7), (*uint)(nil)).Return(nil, nil).Once()

	_, _, err := svc.GetCatalogSnapshot(context.Background(), 7, nil)

	require.Error(t, err)
}

type failingEntitlements struct{}

func (failingEntitlements) Entitlements(context.Context, uint, time.Time) (*subscription.Entitlements, error) {
	return nil, errors.New("boom")
}
