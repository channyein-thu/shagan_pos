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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
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
	return NewService(repo, inv, products, auditWriter, nil, nil, fakeTransactioner{})
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
	svc, repo, _ := newListSalesServiceWithOrgs(t)
	return svc, repo
}

func newListSalesServiceWithOrgs(t *testing.T) (*Service, *MockRepository, *MockOrganizationLookup) {
	repo := NewMockRepository(t)
	orgs := NewMockOrganizationLookup(t)
	svc := NewService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t), orgs, NewMockReturnActivityReader(t), fakeTransactioner{})
	return svc, repo, orgs
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

	_, err := svc.ListSales(context.Background(), 7, &branch, &from, &to, 1, 20)
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

	_, err := svc.ListSales(context.Background(), 7, nil, &from, nil, 1, 20)
	require.NoError(t, err)
}

// Only a date bound needs the zone; an unfiltered list must not pay for the lookup.
func TestService_ListSales_NoDateBounds_DoesNotLookUpTheOrg(t *testing.T) {
	svc, repo, _ := newListSalesServiceWithOrgs(t) // no GetOrganization expectation: a call would fail the test
	repo.EXPECT().ListSales(mock.Anything, uint(7), SaleFilter{Page: 1, PageSize: 20}).Return([]Sale{}, int64(0), nil).Once()
	repo.EXPECT().ListPaymentMethods(mock.Anything, mock.Anything).Return(map[uuid.UUID][]PaymentMethod{}, nil).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 1, 20)
	require.NoError(t, err)
}

func TestService_ListSales_OrgLookupFails_PropagatesBeforeQuerying(t *testing.T) {
	svc, _, orgs := newListSalesServiceWithOrgs(t)
	boom := errors.New("db down")
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	orgs.EXPECT().GetOrganization(mock.Anything, uint(7)).Return(nil, boom).Once()

	_, err := svc.ListSales(context.Background(), 7, nil, &from, nil, 1, 20)
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

func newReceiptService(t *testing.T) (*Service, *MockRepository, *MockReturnActivityReader) {
	repo := NewMockRepository(t)
	activity := NewMockReturnActivityReader(t)
	svc := NewService(repo, NewMockInventoryWriter(t), NewMockProductLookup(t), NewMockAuditWriter(t), nil, activity, fakeTransactioner{})
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

	got, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 1, 20)

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

	_, err := svc.ListSales(context.Background(), 7, nil, nil, nil, 1, 20)

	require.ErrorIs(t, err, boom)
}
