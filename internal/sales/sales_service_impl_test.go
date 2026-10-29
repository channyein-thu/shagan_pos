package sales

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against a mocked Repository. Same pattern as
// identity's and customer's own fakeTransactioner.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func newTestService(repo Repository, inv InventoryWriter, products ProductLookup, auditWriter AuditWriter) *Service {
	return NewService(repo, inv, products, nil, auditWriter, nil, nil, nil, fakeTransactioner{})
}

// noStoredSale makes CreateSale's idempotency lookup find nothing, i.e. a
// first-time sale.
func noStoredSale(repo *MockRepository) {
	repo.EXPECT().GetSale(mock.Anything, mock.Anything, mock.Anything).Return(nil, common.NotFoundError("sale not found")).Once()
}

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func newListSalesService(t *testing.T) (*Service, *MockRepository) {
	svc, repo, _ := newListSalesServiceWithOrgs(t)
	return svc, repo
}

func newListSalesServiceWithOrgs(t *testing.T) (*Service, *MockRepository, *MockOrganizationLookup) {
	repo := NewMockRepository(t)
	orgs := NewMockOrganizationLookup(t)
	svc := NewService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), nil, NewMockAuditWriter(t), orgs, NewMockReturnActivityReader(t), NewMockStaffDirectory(t), fakeTransactioner{})
	return svc, repo, orgs
}

func TestService_ListSales_DefaultsPaginationAndLeavesFiltersOpen(t *testing.T) {
	svc, repo := newListSalesService(t)

	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 1, PageSize: 20}).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, got.Page)
	require.Equal(t, 20, got.PageSize)
	require.NotNil(t, got.Sales, "an empty page must serialize as [], not null")
	require.Empty(t, got.Sales)
}

func TestService_ListSales_ClampsPageSizeToMax(t *testing.T) {
	svc, repo := newListSalesService(t)

	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 3, PageSize: 100}).Return([]Sale{}, int64(250), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 3, 5000)
	require.NoError(t, err)
	require.Equal(t, 100, got.PageSize)
	require.Equal(t, int64(250), got.TotalCount)
}

// from/to are inclusive calendar dates in the ORG's zone: Oct 1..Oct 7 in Yangon
// (UTC+6:30) is [Sep 30 17:30Z, Oct 7 17:30Z) - to's whole day is included and
// a time-of-day on the input is ignored (backend-recommendations #9).
func TestService_ListSales_DatesAreOrgLocalDaysAndBranchPassesThrough(t *testing.T) {
	svc, repo, orgs := newListSalesServiceWithOrgs(t)

	branch := uint(5)
	from := time.Date(2026, 10, 1, 17, 30, 0, 0, time.UTC)
	to := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	wantStart := time.Date(2026, 9, 30, 17, 30, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 7, 17, 30, 0, 0, time.UTC)

	orgs.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&identity.Organization{ID: 7, Timezone: "Asia/Yangon"}, nil).Once()
	repo.EXPECT().ListSales(mock.Anything, uint(7), mock.MatchedBy(func(f SaleFilter) bool {
		return f.BranchID != nil && *f.BranchID == 5 && f.Start != nil && f.Start.Equal(wantStart) &&
			f.End != nil && f.End.Equal(wantEnd) && f.Page == 1 && f.PageSize == 20
	})).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, &branch, nil, nil, &from, &to, 1, 20)
	require.NoError(t, err)
}

func TestService_ListSales_DifferentOrgZoneMovesTheWindow(t *testing.T) {
	svc, repo, orgs := newListSalesServiceWithOrgs(t)

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wantStart := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC) // Bangkok is UTC+7
	orgs.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(&identity.Organization{ID: 7, Timezone: "Asia/Bangkok"}, nil).Once()
	repo.EXPECT().ListSales(mock.Anything, uint(7), mock.MatchedBy(func(f SaleFilter) bool {
		return f.Start != nil && f.Start.Equal(wantStart) && f.End == nil
	})).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, &from, nil, 1, 20)
	require.NoError(t, err)
}

// Only a date bound needs the zone; an unfiltered list must not pay for the lookup.
func TestService_ListSales_NoDateBounds_DoesNotLookUpTheOrg(t *testing.T) {
	svc, repo, _ := newListSalesServiceWithOrgs(t) // no GetOrganization expectation: a call would fail the test
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 1, PageSize: 20}).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 1, 20)
	require.NoError(t, err)
}

func TestService_ListSales_OrgLookupFails_PropagatesBeforeQuerying(t *testing.T) {
	svc, _, orgs := newListSalesServiceWithOrgs(t)
	boom := errors.New("db down")
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	orgs.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(nil, boom).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, &from, nil, 1, 20)
	require.ErrorIs(t, err, boom)
}

func TestService_ListSales_AttachesPaymentMethodsPerRow(t *testing.T) {
	svc, repo := newListSalesService(t)

	cashOnly, split, none := uuid.New(), uuid.New(), uuid.New()
	repo.EXPECT().ListSales(mock.Anything, uint(7), mock.Anything).
		Return([]Sale{{ID: cashOnly, OrgID: 7}, {ID: split, OrgID: 7}, {ID: none, OrgID: 7}}, int64(3), nil).Once()
	// One query for the whole page, not one per row.
	repo.EXPECT().ListPaymentMethods(mock.Anything, []uuid.UUID{cashOnly, split, none}).
		Return(map[uuid.UUID][]PaymentMethod{
			cashOnly: {PaymentMethodCash},
			split:    {PaymentMethodCash, PaymentMethodQR},
		}, nil).Once()
	svc.returns.(*MockReturnActivityReader).EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{cashOnly, split, none}).
		Return(map[uuid.UUID]ReturnSummary{}, nil).Once()

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 1, 20)
	require.NoError(t, err)
	require.Len(t, got.Sales, 3)
	require.Equal(t, []PaymentMethod{PaymentMethodCash}, got.Sales[0].PaymentMethods)
	require.Equal(t, []PaymentMethod{PaymentMethodCash, PaymentMethodQR}, got.Sales[1].PaymentMethods)
	require.Equal(t, []PaymentMethod{}, got.Sales[2].PaymentMethods, "a sale with no payment rows is [], not null")
	require.Equal(t, cashOnly, got.Sales[0].ID)
}

func TestService_ListSales_RepositoryErrorsPropagate(t *testing.T) {
	boom := errors.New("db down")

	t.Run("list", func(t *testing.T) {
		svc, repo := newListSalesService(t)
		repo.EXPECT().ListSales(mock.Anything, uint(7), mock.Anything).Return(nil, int64(0), boom).Once()
		_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 1, 20)
		require.ErrorIs(t, err, boom)
	})
	t.Run("payment methods", func(t *testing.T) {
		svc, repo := newListSalesService(t)
		repo.EXPECT().ListSales(mock.Anything, uint(7), mock.Anything).Return([]Sale{{ID: uuid.New()}}, int64(1), nil).Once()
		repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(nil, boom).Once()
		_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 1, 20)
		require.ErrorIs(t, err, boom)
	})
}

func TestService_GetSale_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	id := uuid.New()
	want := &Sale{ID: id, OrgID: 7}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(want, nil).Once()

	got, err := svc.GetSale(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_GetSaleReceipt_BundlesSaleItemsAndPayments(t *testing.T) {
	svc, repo, activity := newReceiptService(t)

	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7}
	items := []SaleItem{{SaleID: id}}
	payments := []Payment{{SaleID: id}}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(items, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(payments, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, mock.Anything).Return(map[uint]int{}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{id}).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()

	got, err := svc.GetSaleReceipt(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, ReceiptSale{Sale: *sale}, got["sale"])
	require.Equal(t, []ReceiptItem{{SaleItem: items[0]}}, got["items"])
	require.Equal(t, payments, got["payments"])
}

func TestService_GetSaleReceipt_PropagatesNotFoundWithoutListingItemsOrPayments(t *testing.T) {
	svc, repo, _ := newReceiptService(t)

	id := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(nil, common.NotFoundError("sale not found")).Once()

	_, err := svc.GetSaleReceipt(context.Background(), 7, id)
	requireRestErrorStatus(t, err, http.StatusNotFound)
	repo.AssertNotCalled(t, "ListSaleItems", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ListPayments", mock.Anything, mock.Anything)
}

func TestService_ReprintSale_ReturnsSameBundleAsGetSaleReceipt(t *testing.T) {
	svc, repo, activity := newReceiptService(t)

	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(nil, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(nil, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, mock.Anything).Return(map[uint]int{}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{id}).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()

	got, err := svc.ReprintSale(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, ReceiptSale{Sale: *sale}, got["sale"])
}

func TestService_CreateHeldSale_OverwritesBranchStaffAndHeldAt(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	items := datatypes.JSON(`[{"product_id":1,"qty":2}]`)
	in := CreateHeldSaleRequest{
		BranchID: 999, StaffID: 999, Items: items, Discount: decimal.NewFromInt(50),
	}
	want := &HeldSale{ID: 1, BranchID: 3, StaffID: 14}
	repo.EXPECT().CreateHeldSale(mock.Anything, mock.MatchedBy(func(got CreateHeldSaleRequest) bool {
		return got.BranchID == 3 && got.StaffID == 14 && !got.HeldAt.IsZero() && string(got.Items) == string(items)
	})).Return(want, nil).Once()

	got, err := svc.CreateHeldSale(context.Background(), 3, 14, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_CreateHeldSale_RejectsNegativeDiscountWithoutPersisting(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	in := CreateHeldSaleRequest{Items: datatypes.JSON(`[]`), Discount: decimal.NewFromInt(-1)}

	_, err := svc.CreateHeldSale(context.Background(), 3, 14, in)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "CreateHeldSale", mock.Anything, mock.Anything)
}

func TestService_ListHeldSales_DelegatesToRepositoryByBranch(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	want := []HeldSale{{ID: 1, BranchID: 3}, {ID: 2, BranchID: 3}}
	repo.EXPECT().ListHeldSales(mock.Anything, uint(3)).Return(want, nil).Once()

	got, err := svc.ListHeldSales(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ResumeHeldSale_DelegatesToRepositoryByBranch(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	want := &HeldSale{ID: 9, BranchID: 3}
	repo.EXPECT().ResumeHeldSale(mock.Anything, uint(3), uint(9)).Return(want, nil).Once()

	got, err := svc.ResumeHeldSale(context.Background(), 3, 9)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_ResumeHeldSale_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	repo.EXPECT().ResumeHeldSale(mock.Anything, uint(3), uint(9)).Return(nil, common.NotFoundError("held sale not found")).Once()

	_, err := svc.ResumeHeldSale(context.Background(), 3, 9)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func newReceiptService(t *testing.T) (*Service, *MockRepository, *MockReturnActivityReader) {
	repo := NewMockRepository(t)
	activity := NewMockReturnActivityReader(t)
	svc := NewService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), nil, NewMockAuditWriter(t), nil, activity, NewMockStaffDirectory(t), fakeTransactioner{})
	return svc, repo, activity
}

// receiptFor wires one sale with the given lines and the return activity the
// reader reports for them, and returns the receipt.
func receiptFor(t *testing.T, lines []SaleItem, returnedQty map[uint]int, summary map[uuid.UUID]ReturnSummary) (uuid.UUID, map[string]any) {
	t.Helper()
	svc, repo, activity := newReceiptService(t)
	saleID := uuid.New()
	for i := range lines {
		lines[i].SaleID = saleID
	}
	ids := make([]uint, len(lines))
	for i, l := range lines {
		ids[i] = l.ID
	}
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, Status: SaleStatusCompleted}, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, saleID).Return(lines, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, saleID).Return([]Payment{}, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, ids).Return(returnedQty, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{saleID}).Return(summary, nil).Once()

	got, err := svc.GetSaleReceipt(context.Background(), 7, saleID)
	require.NoError(t, err)
	return saleID, got
}

func receiptItems(t *testing.T, receipt map[string]any) []ReceiptItem {
	t.Helper()
	items, ok := receipt["items"].([]ReceiptItem)
	require.True(t, ok, "items should be []ReceiptItem, got %T", receipt["items"])
	return items
}

func TestService_GetSaleReceipt_FreshSale_EverythingIsReturnable(t *testing.T) {
	_, got := receiptFor(t, []SaleItem{{ID: 1, Qty: 2}, {ID: 2, Qty: 5}}, map[uint]int{}, map[uuid.UUID]ReturnSummary{})

	items := receiptItems(t, got)
	require.Len(t, items, 2)
	require.Equal(t, 0, items[0].ReturnedQty)
	require.Equal(t, 2, items[0].ReturnableQty)
	require.Equal(t, 0, items[1].ReturnedQty)
	require.Equal(t, 5, items[1].ReturnableQty)
}

func TestService_GetSaleReceipt_PartiallyReturned_ReturnableIsWhatRemains(t *testing.T) {
	_, got := receiptFor(t, []SaleItem{{ID: 1, Qty: 2}, {ID: 2, Qty: 5}}, map[uint]int{1: 1}, map[uuid.UUID]ReturnSummary{})

	items := receiptItems(t, got)
	require.Equal(t, 1, items[0].ReturnedQty)
	require.Equal(t, 1, items[0].ReturnableQty)
	// The untouched line is unaffected.
	require.Equal(t, 0, items[1].ReturnedQty)
	require.Equal(t, 5, items[1].ReturnableQty)
}

// The reader already adds exchange "in" lines to return lines, so a line
// that was returned once and exchanged in for the rest reads as fully back.
func TestService_GetSaleReceipt_ReturnedPlusExchangedInTheRest_ReturnableIsZero(t *testing.T) {
	_, got := receiptFor(t, []SaleItem{{ID: 1, Qty: 2}}, map[uint]int{1: 2}, map[uuid.UUID]ReturnSummary{})

	items := receiptItems(t, got)
	require.Equal(t, 2, items[0].ReturnedQty)
	require.Equal(t, 0, items[0].ReturnableQty)
}

// Bad data (more recorded back than was sold) must not make the till offer a
// negative quantity.
func TestService_GetSaleReceipt_MoreReturnedThanSold_ReturnableStopsAtZero(t *testing.T) {
	_, got := receiptFor(t, []SaleItem{{ID: 1, Qty: 2}}, map[uint]int{1: 3}, map[uuid.UUID]ReturnSummary{})

	require.Equal(t, 0, receiptItems(t, got)[0].ReturnableQty)
}

func TestService_GetSaleReceipt_SaleWithoutActivity_HasNoReturnFlagsAndZeroRefund(t *testing.T) {
	_, got := receiptFor(t, []SaleItem{{ID: 1, Qty: 1}}, map[uint]int{}, map[uuid.UUID]ReturnSummary{})

	sale, ok := got["sale"].(ReceiptSale)
	require.True(t, ok, "sale should be ReceiptSale, got %T", got["sale"])
	require.False(t, sale.HasReturn)
	require.False(t, sale.HasExchange)
	require.True(t, sale.RefundedTotal.IsZero())
}

func TestService_GetSaleReceipt_SaleWithReturnAndExchange_CarriesTheFlagsAndRefundedTotal(t *testing.T) {
	svc, repo, activity := newReceiptService(t)
	saleID := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, Status: SaleStatusCompleted}, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, saleID).Return([]SaleItem{{ID: 1, SaleID: saleID, Qty: 3}}, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, saleID).Return([]Payment{}, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, []uint{1}).Return(map[uint]int{1: 2}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{saleID}).
		Return(map[uuid.UUID]ReturnSummary{saleID: {HasReturn: true, HasExchange: true, RefundedTotal: d("19.75")}}, nil).Once()

	got, err := svc.GetSaleReceipt(context.Background(), 7, saleID)

	require.NoError(t, err)
	sale := got["sale"].(ReceiptSale)
	require.True(t, sale.HasReturn)
	require.True(t, sale.HasExchange)
	require.True(t, d("19.75").Equal(sale.RefundedTotal))
}

// The frontend reads these as flat fields next to the existing ones, so the
// wrapper types must not nest.
func TestService_GetSaleReceipt_JSONShape_FlatAndBackwardCompatible(t *testing.T) {
	saleID, got := receiptFor(t, []SaleItem{{ID: 1, Qty: 2, NameSnapshot: "Juice"}}, map[uint]int{1: 1},
		map[uuid.UUID]ReturnSummary{})
	_ = saleID

	raw, err := json.Marshal(got)
	require.NoError(t, err)
	var decoded struct {
		Sale  map[string]any   `json:"sale"`
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	// existing fields still there...
	require.Contains(t, decoded.Sale, "id")
	require.Contains(t, decoded.Sale, "total")
	require.Equal(t, "Juice", decoded.Items[0]["name_snapshot"])
	require.EqualValues(t, 2, decoded.Items[0]["qty"])
	// ...and the new ones sit beside them.
	require.Equal(t, false, decoded.Sale["has_return"])
	require.Equal(t, false, decoded.Sale["has_exchange"])
	require.Equal(t, "0", decoded.Sale["refunded_total"])
	require.EqualValues(t, 1, decoded.Items[0]["returned_qty"])
	require.EqualValues(t, 1, decoded.Items[0]["returnable_qty"])
}

func TestService_GetSaleReceipt_ReturnActivityFails_PropagatesAsIs(t *testing.T) {
	svc, repo, activity := newReceiptService(t)
	saleID := uuid.New()
	boom := errors.New("connection refused")
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7}, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, saleID).Return([]SaleItem{{ID: 1, SaleID: saleID, Qty: 1}}, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, saleID).Return([]Payment{}, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, []uint{1}).Return(nil, boom).Once()

	_, err := svc.GetSaleReceipt(context.Background(), 7, saleID)

	require.ErrorIs(t, err, boom)
}

// Reprint is the same bundle, so it must carry the same fields.
func TestService_ReprintSale_CarriesTheSameReturnFields(t *testing.T) {
	svc, repo, activity := newReceiptService(t)
	saleID := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7}, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, saleID).Return([]SaleItem{{ID: 1, SaleID: saleID, Qty: 4}}, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, saleID).Return([]Payment{}, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, []uint{1}).Return(map[uint]int{1: 1}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{saleID}).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()

	got, err := svc.ReprintSale(context.Background(), 7, saleID)

	require.NoError(t, err)
	require.Equal(t, 3, receiptItems(t, got)[0].ReturnableQty)
}

func TestService_ListSales_RowsCarryReturnSummary_AndUntouchedSalesDefaultToNone(t *testing.T) {
	svc, repo, orgs := newListSalesServiceWithOrgs(t)
	_ = orgs
	returned, plain := uuid.New(), uuid.New()
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 1, PageSize: 20}).
		Return([]Sale{{ID: returned}, {ID: plain}}, int64(2), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()
	svc.returns.(*MockReturnActivityReader).EXPECT().
		ReturnSummaries(mock.Anything, []uuid.UUID{returned, plain}).
		Return(map[uuid.UUID]ReturnSummary{returned: {HasReturn: true, RefundedTotal: d("12.50")}}, nil).Once()

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 1, 20)

	require.NoError(t, err)
	require.Len(t, got.Sales, 2)
	require.True(t, got.Sales[0].HasReturn)
	require.False(t, got.Sales[0].HasExchange)
	require.True(t, d("12.50").Equal(got.Sales[0].RefundedTotal))
	require.False(t, got.Sales[1].HasReturn)
	require.False(t, got.Sales[1].HasExchange)
	require.True(t, got.Sales[1].RefundedTotal.IsZero())
}

func TestService_ListSales_ReturnSummaryFails_PropagatesAsIs(t *testing.T) {
	svc, repo, _ := newListSalesServiceWithOrgs(t)
	id := uuid.New()
	boom := errors.New("connection refused")
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 1, PageSize: 20}).Return([]Sale{{ID: id}}, int64(1), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()
	svc.returns.(*MockReturnActivityReader).EXPECT().ReturnSummaries(mock.Anything, mock.Anything).Return(nil, boom).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, nil, nil, 1, 20)

	require.ErrorIs(t, err, boom)
}

// A cashier without view_all_receipts only sees the sales they rang up: the
// handler passes their staff id, and it must reach the repository's filter.
func TestService_ListSales_StaffFilterIsPassedToTheRepository(t *testing.T) {
	svc, repo := newListSalesService(t)
	staff := uint(14)
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{StaffID: &staff, Page: 1, PageSize: 20}).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, &staff, nil, nil, nil, 1, 20)

	require.NoError(t, err)
}

// ---- discount breakdown on the receipt ----

func newReceiptServiceWithStaff(t *testing.T) (*Service, *MockRepository, *MockReturnActivityReader, *MockStaffDirectory) {
	repo := NewMockRepository(t)
	activity := NewMockReturnActivityReader(t)
	staff := NewMockStaffDirectory(t)
	svc := NewService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), nil, NewMockAuditWriter(t), nil, activity, staff, fakeTransactioner{})
	return svc, repo, activity, staff
}

func receiptOf(t *testing.T, svc *Service, repo *MockRepository, activity *MockReturnActivityReader, lines []SaleItem) []ReceiptItem {
	t.Helper()
	saleID := uuid.New()
	ids := make([]uint, len(lines))
	for i := range lines {
		lines[i].SaleID = saleID
		ids[i] = lines[i].ID
	}
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7}, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, saleID).Return(lines, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, saleID).Return([]Payment{}, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, ids).Return(map[uint]int{}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{saleID}).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()
	got, err := svc.GetSaleReceipt(context.Background(), 7, saleID)
	require.NoError(t, err)
	return receiptItems(t, got)
}

func uintPtr(v uint) *uint { return &v }

func TestService_GetSaleReceipt_ManualDiscountLine_ShowsOriginalAndSalePriceAndWhoApproved(t *testing.T) {
	svc, repo, activity, staff := newReceiptServiceWithStaff(t)
	approval := uuid.New()
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, nil).Once()

	items := receiptOf(t, svc, repo, activity, []SaleItem{{
		ID: 1, Qty: 2, UnitPrice: d("6500"), ManualDiscountPercent: d("10"), ManualDiscount: d("1200"),
		ManualDiscountReason: "damaged box", Discount: d("1200"), LineTotal: d("11800"),
		DiscountApprovalID: &approval, ManualApprovedBy: uintPtr(17),
	}})

	it := items[0]
	require.True(t, d("6500").Equal(it.UnitPrice), "original price")
	require.True(t, d("5900").Equal(it.UnitSalePrice), "6,500 - 1,200 / 2")
	require.True(t, d("10").Equal(it.ManualDiscountPercent))
	require.Equal(t, "damaged box", it.ManualDiscountReason)
	require.Equal(t, "Ko Zaw", it.ManualApprovedByName)
	require.Equal(t, uint(17), *it.ManualApprovedBy)
}

func TestService_GetSaleReceipt_CatalogDiscountLine_ShowsTheCatalogPercentAndNoApprover(t *testing.T) {
	svc, repo, activity, _ := newReceiptServiceWithStaff(t) // GetStaff must not be called

	items := receiptOf(t, svc, repo, activity, []SaleItem{{
		ID: 1, Qty: 1, UnitPrice: d("6500"), CatalogDiscountPercent: d("10"), CatalogDiscount: d("600"),
		Discount: d("600"), LineTotal: d("5900"),
	}})

	require.True(t, d("5900").Equal(items[0].UnitSalePrice))
	require.True(t, d("10").Equal(items[0].CatalogDiscountPercent))
	require.Equal(t, "", items[0].ManualApprovedByName)
}

func TestService_GetSaleReceipt_ComboLine_ShowsTheComboDiscountSeparately(t *testing.T) {
	svc, repo, activity, _ := newReceiptServiceWithStaff(t)

	items := receiptOf(t, svc, repo, activity, []SaleItem{{
		ID: 1, Qty: 1, UnitPrice: d("600"), ComboDiscount: d("40"), Discount: d("40"), LineTotal: d("560"),
	}})

	require.True(t, d("40").Equal(items[0].ComboDiscount))
	require.True(t, d("600").Equal(items[0].UnitSalePrice), "a combo share is not a unit-price discount")
}

// A line rung up before the discount was split by source has only `discount`:
// it must still render without inventing a breakdown.
func TestService_GetSaleReceipt_LegacyLine_KeepsItsDiscountAndOriginalPrice(t *testing.T) {
	svc, repo, activity, _ := newReceiptServiceWithStaff(t)

	items := receiptOf(t, svc, repo, activity, []SaleItem{{
		ID: 1, Qty: 2, UnitPrice: d("1000"), Discount: d("50"), LineTotal: d("1950"),
	}})

	require.True(t, d("50").Equal(items[0].Discount))
	require.True(t, d("1000").Equal(items[0].UnitSalePrice))
	require.True(t, items[0].CatalogDiscount.IsZero() && items[0].ManualDiscount.IsZero())
}

func TestService_GetSaleReceipt_SameApproverOnSeveralLines_IsLookedUpOnce(t *testing.T) {
	svc, repo, activity, staff := newReceiptServiceWithStaff(t)
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, nil).Once()
	line := func(id uint) SaleItem {
		return SaleItem{ID: id, Qty: 1, UnitPrice: d("100"), ManualDiscount: d("10"), Discount: d("10"), ManualApprovedBy: uintPtr(17)}
	}

	items := receiptOf(t, svc, repo, activity, []SaleItem{line(1), line(2)})

	require.Equal(t, "Ko Zaw", items[0].ManualApprovedByName)
	require.Equal(t, "Ko Zaw", items[1].ManualApprovedByName)
}

func TestService_GetSaleReceipt_ApproverNoLongerFindable_StillReturnsTheReceiptWithoutAName(t *testing.T) {
	svc, repo, activity, staff := newReceiptServiceWithStaff(t)
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(nil, common.NotFoundError("staff not found")).Once()

	items := receiptOf(t, svc, repo, activity, []SaleItem{{
		ID: 1, Qty: 1, UnitPrice: d("100"), ManualDiscount: d("10"), Discount: d("10"), ManualApprovedBy: uintPtr(17),
	}})

	require.Equal(t, "", items[0].ManualApprovedByName)
	require.Equal(t, uint(17), *items[0].ManualApprovedBy, "the id is still there")
}

func TestService_GetSaleReceipt_ApproverLookupFails_PropagatesAsIs(t *testing.T) {
	svc, repo, activity, staff := newReceiptServiceWithStaff(t)
	saleID := uuid.New()
	boom := errors.New("db down")
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7}, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, saleID).Return([]SaleItem{{ID: 1, SaleID: saleID, Qty: 1, ManualApprovedBy: uintPtr(17)}}, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, saleID).Return([]Payment{}, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, mock.Anything).Return(map[uint]int{}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, mock.Anything).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(nil, boom).Once()

	_, err := svc.GetSaleReceipt(context.Background(), 7, saleID)

	require.ErrorIs(t, err, boom)
}

func TestService_GetSaleReceipt_JSONShape_CarriesTheDiscountFields(t *testing.T) {
	svc, repo, activity, staff := newReceiptServiceWithStaff(t)
	staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, nil).Once()
	items := receiptOf(t, svc, repo, activity, []SaleItem{{
		ID: 1, Qty: 1, UnitPrice: d("6500"), ManualDiscountPercent: d("10"), ManualDiscount: d("600"),
		ManualDiscountReason: "damaged box", Discount: d("600"), LineTotal: d("5900"), ManualApprovedBy: uintPtr(17),
	}})

	raw, err := json.Marshal(items[0])
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, "6500", got["unit_price"])
	require.Equal(t, "5900", got["unit_sale_price"])
	require.Equal(t, "10", got["manual_discount_percent"])
	require.Equal(t, "damaged box", got["manual_discount_reason"])
	require.Equal(t, "0", got["combo_discount"])
	require.Equal(t, "0", got["catalog_discount_percent"])
	require.EqualValues(t, 17, got["manual_approved_by"])
	require.Equal(t, "Ko Zaw", got["manual_approved_by_name"])
	require.NotContains(t, got, "unit_cost", "cost is never serialized")
}

// ---- receipts: tax is inside the price ----

// A sale made since tax became a rate inside the price says so, so the till
// prints "Includes tax: 500" instead of adding 500 to the total.
func TestService_GetSaleReceipt_TaxInclusiveSale_SaysSoAndCarriesEachLinesRateAndTax(t *testing.T) {
	svc, repo, activity := newReceiptService(t)
	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7, Subtotal: d("10500"), Tax: d("500"), Total: d("10500"), TaxInclusive: true}
	items := []SaleItem{{ID: 1, SaleID: id, Qty: 1, UnitPrice: d("10500"), LineTotal: d("10500"), TaxPercent: d("5"), Tax: d("500")}}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(items, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(nil, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, mock.Anything).Return(map[uint]int{}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{id}).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()

	got, err := svc.GetSaleReceipt(context.Background(), 7, id)

	require.NoError(t, err)
	require.Equal(t, true, got["tax_inclusive"])
	require.True(t, got["sale"].(ReceiptSale).TaxInclusive)
	item := receiptItems(t, got)[0]
	require.True(t, d("5").Equal(item.TaxPercent))
	require.True(t, d("500").Equal(item.Tax))
}

// A sale from before added its tax on top of the total; reprinting it must not
// claim the prices included tax.
func TestService_GetSaleReceipt_SaleFromBeforeInclusiveTax_IsNotMarkedInclusive(t *testing.T) {
	svc, repo, activity := newReceiptService(t)
	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7, Subtotal: d("800"), Tax: d("40"), Total: d("840")}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(nil, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(nil, nil).Once()
	activity.EXPECT().ReturnedQtyBySaleItems(mock.Anything, mock.Anything).Return(map[uint]int{}, nil).Once()
	activity.EXPECT().ReturnSummaries(mock.Anything, []uuid.UUID{id}).Return(map[uuid.UUID]ReturnSummary{}, nil).Once()

	got, err := svc.GetSaleReceipt(context.Background(), 7, id)

	require.NoError(t, err)
	require.Equal(t, false, got["tax_inclusive"])
	require.True(t, d("840").Equal(got["sale"].(ReceiptSale).Total), "stored totals are never recomputed")
}

func TestService_ListSales_PassesTheShiftFilterToTheRepository(t *testing.T) {
	svc, repo, _ := newListSalesServiceWithOrgs(t)
	shift := uint(412)
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{ShiftID: &shift, Page: 1, PageSize: 20}).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, nil, &shift, nil, nil, 1, 20)

	require.NoError(t, err)
}
