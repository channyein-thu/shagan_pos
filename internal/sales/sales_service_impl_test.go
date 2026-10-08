package sales

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
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
	return NewService(repo, inv, products, auditWriter, fakeTransactioner{})
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

func TestService_CreateSale_HappyPath_DerivesTotalsAndPersistsAtomically(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	saleID := uuid.New()
	in := CreateSaleRequest{
		ID:       saleID,
		ShiftID:  9,
		StaffID:  14,
		DeviceID: 3,
		Items: []CreateSaleItemRequest{
			{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 2, Discount: decimal.NewFromInt(50), Tax: decimal.NewFromInt(100)},
		},
		Payments: []CreateSalePaymentRequest{
			{Method: PaymentMethodCash, Amount: decimal.NewFromInt(2050), AmountReceived: decimal.NewFromInt(2050)},
		},
	}
	// subtotal = 1000*2 = 2000; discount = 50; tax = 100; total = 2000 - 50 + 100 = 2050

	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, CostPrice: d("600.00")}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.MatchedBy(func(s *Sale) bool {
		return s.ID == saleID && s.OrgID == 7 && s.BranchID == 3 && s.ShiftID == 9 && s.StaffID == 14 && s.DeviceID == 3 &&
			s.Subtotal.Equal(decimal.NewFromInt(2000)) &&
			s.Discount.Equal(decimal.NewFromInt(50)) &&
			s.Tax.Equal(decimal.NewFromInt(100)) &&
			s.Total.Equal(decimal.NewFromInt(2050)) &&
			s.Status == SaleStatusCompleted && s.CompletedAt != nil
	})).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.MatchedBy(func(items []SaleItem) bool {
		return len(items) == 1 && items[0].SaleID == saleID && items[0].LineTotal.Equal(decimal.NewFromInt(1950)) &&
			items[0].UnitCost.Equal(d("600.00"))
	})).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.MatchedBy(func(payments []Payment) bool {
		return len(payments) == 1 && payments[0].SaleID == saleID && payments[0].Amount.Equal(decimal.NewFromInt(2050))
	})).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 50, ProductID: 1, BranchID: 3, Qty: 10}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 8).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.OrgID == 7 && e.ProductID == 1 && e.BranchID == 3 && e.Type == inventory.LedgerEntryTypeSale &&
				e.Qty == -2 && e.BalanceAfter == 8 && e.ActorID != nil && *e.ActorID == 14 &&
				e.ReferenceType == inventory.ReferenceTypeSale && e.ReferenceID == saleID.String()
		})).
		Return(nil).Once()
	audW.EXPECT().
		CreateAuditLog(mock.Anything, mock.MatchedBy(func(e *audit.AuditLog) bool {
			return e.OrgID == 7 && e.ActorID != nil && *e.ActorID == 14 && e.BranchID != nil && *e.BranchID == 3 &&
				e.Entity == "sale" && e.EntityID == saleID.String() && e.Action == "manual_discount_applied"
		})).
		Return(nil).Once()

	actor := SaleActor{StaffID: 14, CanApplyManualDiscount: true}
	got, _, err := svc.CreateSale(context.Background(), 7, 3, actor, in, false)
	require.NoError(t, err)
	require.True(t, got.Total.Equal(decimal.NewFromInt(2050)))
}

func TestService_CreateSale_UsesPriceOverrideWhenSet(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	override := decimal.NewFromInt(800)
	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items: []CreateSaleItemRequest{
			{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), PriceOverride: &override, Qty: 1},
		},
		Payments: []CreateSalePaymentRequest{
			{Method: PaymentMethodCash, Amount: decimal.NewFromInt(800)},
		},
	}

	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, CostPrice: d("400.00")}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.MatchedBy(func(s *Sale) bool {
		return s.Subtotal.Equal(decimal.NewFromInt(800)) && s.Total.Equal(decimal.NewFromInt(800))
	})).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 60, ProductID: 1, BranchID: 3, Qty: 5}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(60), 4).Return(nil).Once()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	require.NoError(t, err)
}

func TestService_CreateSale_PaymentsMismatch_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(500)}},
	}

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_DiscountWithoutPermission_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1, Discount: decimal.NewFromInt(50)}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(950)}},
	}

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14, CanApplyManualDiscount: false}, in, false)
	requireRestErrorStatus(t, err, http.StatusForbidden)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_NegativeItemDiscount_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1, Discount: decimal.NewFromInt(-10)}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(1010)}},
	}

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14, CanApplyManualDiscount: true}, in, false)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_NegativeItemTax_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1, Tax: decimal.NewFromInt(-5)}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(995)}},
	}

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_SumsComboItemTaxIntoSaleTax(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	saleID := uuid.New()
	comboID := uint(5)
	in := CreateSaleRequest{
		ID:      saleID,
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items: []CreateSaleItemRequest{
			{ProductID: 1, ComboID: &comboID, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(600), Qty: 1, Tax: decimal.NewFromInt(20)},
			{ProductID: 2, ComboID: &comboID, NameSnapshot: "Cooking Oil", UnitPrice: decimal.NewFromInt(400), Qty: 1, Tax: decimal.NewFromInt(10)},
		},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(1030)}},
	}
	// subtotal = 600+400 = 1000; tax = 20+10 = 30; total = 1030

	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, CostPrice: d("300.00")}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&catalog.Product{ID: 2, OrgID: 7, CostPrice: d("200.00")}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.MatchedBy(func(s *Sale) bool {
		return s.Tax.Equal(decimal.NewFromInt(30)) && s.Total.Equal(decimal.NewFromInt(1030))
	})).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.MatchedBy(func(items []SaleItem) bool {
		return len(items) == 2 && items[0].ComboID != nil && *items[0].ComboID == comboID && items[1].ComboID != nil && *items[1].ComboID == comboID
	})).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 61, ProductID: 1, BranchID: 3, Qty: 5}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(61), 4).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(3)).Return(&inventory.StockLevel{ID: 62, ProductID: 2, BranchID: 3, Qty: 5}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(62), 4).Return(nil).Once()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Twice()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	require.NoError(t, err)
}

func TestService_CreateSale_InsufficientStock_RollsBackWithConflict(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 5}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(5000)}},
	}

	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 70, ProductID: 1, BranchID: 3, Qty: 2}, nil).Once()
	// UpdateStockLevelQty/CreateInventoryLedgerEntry must never be called -
	// the movement is rejected before either write happens. fakeTransactioner
	// doesn't actually roll back a real DB, but the real *gorm.DB.Transaction
	// this stands in for does when the closure returns an error.

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateSale_AllowNegativeStock_LetsItThroughAndReportsEvent(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 5}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(5000)}},
	}

	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 70, ProductID: 1, BranchID: 3, Qty: 2}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(70), -3).Return(nil).Once()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
		return e.BalanceAfter == -3
	})).Return(nil).Once()

	_, events, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, true)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, NegativeStockEvent{ProductID: 1, BranchID: 3, ResultingQty: -3}, events[0])
}

func TestService_CreateSale_SameProductAcrossMultipleLines_AggregatesQtyBeforeDecrementing(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items: []CreateSaleItemRequest{
			{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(500), Qty: 2},
			{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(500), Qty: 3},
		},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(2500)}},
	}

	// Exactly one GetProduct call for product 1 - the cost lookup is
	// deduped per unique product, same as the stock decrement below.
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, CostPrice: d("300.00")}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.MatchedBy(func(items []SaleItem) bool {
		return len(items) == 2 && items[0].UnitCost.Equal(d("300.00")) && items[1].UnitCost.Equal(d("300.00"))
	})).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()
	// Exactly one GetStockLevel/UpdateStockLevelQty pair for product 1,
	// decrementing by the combined qty of both lines (5), not two separate
	// decrements of 2 and 3.
	inv.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(3)).Return(&inventory.StockLevel{ID: 71, ProductID: 1, BranchID: 3, Qty: 10}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(71), 5).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.ProductID == 1 && e.Qty == -5 && e.BalanceAfter == 5
		})).
		Return(nil).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	require.NoError(t, err)
}

func TestService_CreateSale_ShiftNotOpen_PropagatesErrorWithoutPersisting(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)
	noStoredSale(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(1000)}},
	}

	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).
		Return(common.NotFoundError("shift not found, not open, or doesn't belong to this branch")).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in, false)
	requireRestErrorStatus(t, err, http.StatusNotFound)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func newListSalesService(t *testing.T) (*Service, *MockRepository) {
	repo := NewMockRepository(t)
	return newTestService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t)), repo
}

func TestService_ListSales_DefaultsPaginationAndLeavesFiltersOpen(t *testing.T) {
	svc, repo := newListSalesService(t)

	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 1, PageSize: 20}).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 0, 0)
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

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 3, 5000)
	require.NoError(t, err)
	require.Equal(t, 100, got.PageSize)
	require.Equal(t, int64(250), got.TotalCount)
}

// from/to are inclusive calendar dates: to's whole day is included, and a
// time-of-day on the input is ignored.
func TestService_ListSales_TurnsDatesIntoHalfOpenUTCWindowAndPassesBranch(t *testing.T) {
	svc, repo := newListSalesService(t)

	branch := uint(5)
	from := time.Date(2026, 10, 1, 17, 30, 0, 0, time.UTC)
	to := time.Date(2026, 10, 7, 3, 0, 0, 0, time.UTC)
	wantStart := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{BranchID: &branch, Start: &wantStart, End: &wantEnd, Page: 1, PageSize: 20}).
		Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, &branch, &from, &to, 1, 20)
	require.NoError(t, err)
}

func TestService_ListSales_OnlyFromSet_LeavesEndOpen(t *testing.T) {
	svc, repo := newListSalesService(t)

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Start: &from, Page: 1, PageSize: 20}).
		Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, &from, nil, 1, 20)
	require.NoError(t, err)
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

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 1, 20)
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
		_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 1, 20)
		require.ErrorIs(t, err, boom)
	})
	t.Run("payment methods", func(t *testing.T) {
		svc, repo := newListSalesService(t)
		repo.EXPECT().ListSales(mock.Anything, uint(7), mock.Anything).Return([]Sale{{ID: uuid.New()}}, int64(1), nil).Once()
		repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(nil, boom).Once()
		_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 1, 20)
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
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7}
	items := []SaleItem{{SaleID: id}}
	payments := []Payment{{SaleID: id}}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(items, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(payments, nil).Once()

	got, err := svc.GetSaleReceipt(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, sale, got["sale"])
	require.Equal(t, items, got["items"])
	require.Equal(t, payments, got["payments"])
}

func TestService_GetSaleReceipt_PropagatesNotFoundWithoutListingItemsOrPayments(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	id := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(nil, common.NotFoundError("sale not found")).Once()

	_, err := svc.GetSaleReceipt(context.Background(), 7, id)
	requireRestErrorStatus(t, err, http.StatusNotFound)
	repo.AssertNotCalled(t, "ListSaleItems", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ListPayments", mock.Anything, mock.Anything)
}

func TestService_ReprintSale_ReturnsSameBundleAsGetSaleReceipt(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(nil, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(nil, nil).Once()

	got, err := svc.ReprintSale(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, sale, got["sale"])
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

// --- idempotency (backend-recommendations #4) ---

func replayRequest(saleID uuid.UUID) CreateSaleRequest {
	return CreateSaleRequest{
		ID: saleID, ShiftID: 9, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice", UnitPrice: decimal.NewFromInt(100), Qty: 1}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(100)}},
	}
}

func TestService_CreateSale_AlreadyStoredFromSameDevice_ReturnsOriginalWithoutWriting(t *testing.T) {
	repo := NewMockRepository(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, inv, products, audW)

	saleID := uuid.New()
	stored := &Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 3, StaffID: 14, Total: decimal.NewFromInt(100)}
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(stored, nil).Once()
	// No product lookup, shift check, insert, stock or audit call: a retry is a pure read.

	// A discount-less retry by a staff member lacking the discount permission
	// still works - the original already passed whatever checks it needed.
	got, negs, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, replayRequest(saleID), false)
	require.NoError(t, err)
	require.Empty(t, negs)
	require.Same(t, stored, got)
	require.True(t, got.Replayed)
}

func TestService_CreateSale_AlreadyStoredWithDiscount_RetryNeedsNoFreshApproval(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t))

	saleID := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 3, Discount: decimal.NewFromInt(10)}, nil).Once()

	in := replayRequest(saleID)
	in.Items[0].Discount = decimal.NewFromInt(10)
	// The manager-approval token has expired by retry time: CanApplyManualDiscount is false.
	got, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14, CanApplyManualDiscount: false}, in, false)
	require.NoError(t, err)
	require.True(t, got.Replayed)
}

func TestService_CreateSale_AlreadyStoredFromDifferentDevice_Conflicts(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t))

	saleID := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 99}, nil).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, replayRequest(saleID), false)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateSale_AlreadyStoredAtDifferentBranch_Conflicts(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t))

	saleID := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(&Sale{ID: saleID, OrgID: 7, BranchID: 8, DeviceID: 3}, nil).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, replayRequest(saleID), false)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateSale_LookupFails_PropagatesWithoutWriting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t))

	boom := errors.New("db down")
	repo.EXPECT().GetSale(mock.Anything, uint(7), mock.Anything).Return(nil, boom).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, replayRequest(uuid.New()), false)
	require.ErrorIs(t, err, boom)
}

// Two retries race past the lookup; the loser's insert hits the primary key.
// It must resolve to the winner's stored sale, not a 500.
func TestService_CreateSale_RaceLoser_ResolvesToStoredSale(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, NewMockInventoryWriter(t), products, NewMockAuditWriter(t))

	saleID := uuid.New()
	stored := &Sale{ID: saleID, OrgID: 7, BranchID: 3, DeviceID: 3}
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(nil, common.NotFoundError("sale not found")).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(&pgconn.PgError{Code: "23505"}).Once()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(stored, nil).Once()

	got, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, replayRequest(saleID), false)
	require.NoError(t, err)
	require.True(t, got.Replayed)
}

// The duplicate key belongs to another org's sale: this org can't see it, and
// must not be handed it or told more than "already exists".
func TestService_CreateSale_RaceLoser_IdOwnedByAnotherOrg_Conflicts(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	svc := newTestService(repo, NewMockInventoryWriter(t), products, NewMockAuditWriter(t))

	saleID := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), saleID).Return(nil, common.NotFoundError("sale not found")).Twice()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.Anything).Return(&pgconn.PgError{Code: "23505"}).Once()

	_, _, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, replayRequest(saleID), false)
	requireRestErrorStatus(t, err, http.StatusConflict)
}
