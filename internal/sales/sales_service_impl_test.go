package sales

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against a mocked Repository. Same pattern as
// identity's and customer's own fakeTransactioner.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

func newTestService(repo Repository) *Service {
	return NewService(repo, fakeTransactioner{})
}

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func TestService_CreateSale_HappyPath_DerivesTotalsAndPersistsAtomically(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

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
		return len(items) == 1 && items[0].SaleID == saleID && items[0].LineTotal.Equal(decimal.NewFromInt(1950))
	})).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.MatchedBy(func(payments []Payment) bool {
		return len(payments) == 1 && payments[0].SaleID == saleID && payments[0].Amount.Equal(decimal.NewFromInt(2050))
	})).Return(nil).Once()

	actor := SaleActor{StaffID: 14, CanApplyManualDiscount: true}
	got, err := svc.CreateSale(context.Background(), 7, 3, actor, in)
	require.NoError(t, err)
	require.True(t, got.Total.Equal(decimal.NewFromInt(2050)))
}

func TestService_CreateSale_UsesPriceOverrideWhenSet(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

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

	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.MatchedBy(func(s *Sale) bool {
		return s.Subtotal.Equal(decimal.NewFromInt(800)) && s.Total.Equal(decimal.NewFromInt(800))
	})).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in)
	require.NoError(t, err)
}

func TestService_CreateSale_PaymentsMismatch_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(500)}},
	}

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_DiscountWithoutPermission_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1, Discount: decimal.NewFromInt(50)}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(950)}},
	}

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14, CanApplyManualDiscount: false}, in)
	requireRestErrorStatus(t, err, http.StatusForbidden)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_NegativeItemDiscount_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1, Discount: decimal.NewFromInt(-10)}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(1010)}},
	}

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14, CanApplyManualDiscount: true}, in)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_NegativeItemTax_RejectsWithoutOpeningTransaction(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1, Tax: decimal.NewFromInt(-5)}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(995)}},
	}

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	repo.AssertNotCalled(t, "RequireOpenShift", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_CreateSale_SumsComboItemTaxIntoSaleTax(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

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

	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).Return(nil).Once()
	repo.EXPECT().CreateSale(mock.Anything, mock.MatchedBy(func(s *Sale) bool {
		return s.Tax.Equal(decimal.NewFromInt(30)) && s.Total.Equal(decimal.NewFromInt(1030))
	})).Return(nil).Once()
	repo.EXPECT().CreateSaleItems(mock.Anything, mock.MatchedBy(func(items []SaleItem) bool {
		return len(items) == 2 && items[0].ComboID != nil && *items[0].ComboID == comboID && items[1].ComboID != nil && *items[1].ComboID == comboID
	})).Return(nil).Once()
	repo.EXPECT().CreatePayments(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in)
	require.NoError(t, err)
}

func TestService_CreateSale_ShiftNotOpen_PropagatesErrorWithoutPersisting(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateSaleRequest{
		ID:      uuid.New(),
		ShiftID: 9, StaffID: 14, DeviceID: 3,
		Items:    []CreateSaleItemRequest{{ProductID: 1, NameSnapshot: "Rice 5kg", UnitPrice: decimal.NewFromInt(1000), Qty: 1}},
		Payments: []CreateSalePaymentRequest{{Method: PaymentMethodCash, Amount: decimal.NewFromInt(1000)}},
	}

	repo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(3), uint(9)).
		Return(common.NotFoundError("shift not found, not open, or doesn't belong to this branch")).Once()

	_, err := svc.CreateSale(context.Background(), 7, 3, SaleActor{StaffID: 14}, in)
	requireRestErrorStatus(t, err, http.StatusNotFound)
	repo.AssertNotCalled(t, "CreateSale", mock.Anything, mock.Anything)
}

func TestService_ListSales_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Sale{{OrgID: 7}, {OrgID: 7}}
	repo.EXPECT().ListSales(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListSales(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_GetSale_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	id := uuid.New()
	want := &Sale{ID: id, OrgID: 7}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(want, nil).Once()

	got, err := svc.GetSale(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_GetSaleReceipt_BundlesSaleItemsAndPayments(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

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
	svc := newTestService(repo)

	id := uuid.New()
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(nil, common.NotFoundError("sale not found")).Once()

	_, err := svc.GetSaleReceipt(context.Background(), 7, id)
	requireRestErrorStatus(t, err, http.StatusNotFound)
	repo.AssertNotCalled(t, "ListSaleItems", mock.Anything, mock.Anything)
	repo.AssertNotCalled(t, "ListPayments", mock.Anything, mock.Anything)
}

func TestService_ReprintSale_ReturnsSameBundleAsGetSaleReceipt(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	id := uuid.New()
	sale := &Sale{ID: id, OrgID: 7}
	repo.EXPECT().GetSale(mock.Anything, uint(7), id).Return(sale, nil).Once()
	repo.EXPECT().ListSaleItems(mock.Anything, id).Return(nil, nil).Once()
	repo.EXPECT().ListPayments(mock.Anything, id).Return(nil, nil).Once()

	got, err := svc.ReprintSale(context.Background(), 7, id)
	require.NoError(t, err)
	require.Equal(t, sale, got["sale"])
}
