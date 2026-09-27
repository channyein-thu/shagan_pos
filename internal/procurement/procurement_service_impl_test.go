package procurement

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against mocked dependencies. See catalog's
// equivalent for the same reasoning; *gorm.DB satisfies common.Transactioner
// natively in production.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

func TestService_ListSuppliers_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	want := []Supplier{{ID: 1, OrgID: 7, Name: "Acme Foods"}, {ID: 2, OrgID: 7, Name: "Fresh Produce Co"}}
	repo.EXPECT().ListSuppliers(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListSuppliers(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListSuppliers_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListSuppliers(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListSuppliers(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateSupplier_HappyPath_StampsLastOrderAtAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	before := time.Now()
	repo.EXPECT().
		CreateSupplier(mock.Anything, mock.MatchedBy(func(s *Supplier) bool {
			return s.OrgID == 7 && s.Name == "Acme Foods" && s.Phone == "0123456789" &&
				s.Address == "123 Main St" && !s.LastOrderAt.Before(before)
		})).
		Run(func(_ context.Context, s *Supplier) { s.ID = 1 }).
		Return(nil).
		Once()

	got, err := svc.CreateSupplier(context.Background(), 7, CreateSupplierRequest{
		Name:    "Acme Foods",
		Phone:   "0123456789",
		Address: "123 Main St",
	})
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, uint(7), got.OrgID)
}

func TestService_CreateSupplier_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	dbErr := errors.New("connection refused")
	repo.EXPECT().CreateSupplier(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreateSupplier(context.Background(), 7, CreateSupplierRequest{Name: "Acme Foods", Phone: "0123456789", Address: "123 Main St"})
	require.ErrorIs(t, err, dbErr)
}

func TestService_UpdateSupplier_HappyPath_UpdatesAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &Supplier{ID: 1, OrgID: 7, Name: "Acme Foods"}
	updated := &Supplier{ID: 1, OrgID: 7, Name: "Acme Foods Ltd"}
	name := "Acme Foods Ltd"
	in := UpdateSupplierRequest{Name: &name}

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().UpdateSupplier(mock.Anything, uint(1), map[string]any{"name": "Acme Foods Ltd"}).Return(nil).Once()
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(1)).Return(updated, nil).Once()

	got, err := svc.UpdateSupplier(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, updated, got)
}

func TestService_UpdateSupplier_NoFieldsProvided_SkipsWrite(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &Supplier{ID: 1, OrgID: 7, Name: "Acme Foods"}

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(1)).Return(existing, nil).Twice()
	// UpdateSupplier must never be called - no .EXPECT() set up for it
	// means the mock fails the test if it is.

	got, err := svc.UpdateSupplier(context.Background(), 7, 1, UpdateSupplierRequest{})
	require.NoError(t, err)
	require.Same(t, existing, got)
}

func TestService_UpdateSupplier_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.NotFoundError("supplier not found")
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// UpdateSupplier must never be called on a not-found supplier.

	_, err := svc.UpdateSupplier(context.Background(), 7, 999, UpdateSupplierRequest{})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeleteSupplier_HappyPath_Deletes(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &Supplier{ID: 1, OrgID: 7, Name: "Acme Foods"}
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().PurchaseOrdersExistForSupplier(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().DeleteSupplier(mock.Anything, uint(1)).Return(nil).Once()

	err := svc.DeleteSupplier(context.Background(), 7, 1)
	require.NoError(t, err)
}

func TestService_DeleteSupplier_InUseByPurchaseOrders_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &Supplier{ID: 1, OrgID: 7, Name: "Acme Foods"}
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().PurchaseOrdersExistForSupplier(mock.Anything, uint(1)).Return(true, nil).Once()
	// DeleteSupplier must never be called once a purchase order reference
	// is found - no .EXPECT() set up for it means the mock fails the test
	// if it is.

	err := svc.DeleteSupplier(context.Background(), 7, 1)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_DeleteSupplier_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.NotFoundError("supplier not found")
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// Neither PurchaseOrdersExistForSupplier nor DeleteSupplier must be
	// called on a not-found supplier.

	err := svc.DeleteSupplier(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func validCreatePurchaseOrderRequest() CreatePurchaseOrderRequest {
	return CreatePurchaseOrderRequest{
		PoNumber:   "PO-1001",
		SupplierID: 5,
		Items: []CreatePurchaseOrderItemRequest{
			{ProductID: 1, OrderedQty: 10, UnitCost: decimal.NewFromFloat(2.50)},
			{ProductID: 2, OrderedQty: 5, UnitCost: decimal.NewFromFloat(1.00)},
		},
	}
}

func TestService_CreatePurchaseOrder_HappyPath_CreatesOrderAndItems(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(5)).Return(&Supplier{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&catalog.Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreatePurchaseOrder(mock.Anything, mock.MatchedBy(func(po *PurchaseOrder) bool {
			return po.PoNumber == "PO-1001" && po.SupplierID == 5 && po.Status == PurchaseOrderStatusSubmitted &&
				po.CreatedBy == 42 && po.Total.Equal(decimal.NewFromFloat(30.00))
		})).
		Run(func(_ *gorm.DB, po *PurchaseOrder) { po.ID = 1 }).
		Return(nil).
		Once()
	repo.EXPECT().
		CreatePurchaseOrderItems(mock.Anything, mock.MatchedBy(func(items []PurchaseOrderItem) bool {
			return len(items) == 2 && items[0].PoID == 1 && items[0].ProductID == 1 && items[0].OrderedQty == 10 &&
				items[1].PoID == 1 && items[1].ProductID == 2 && items[1].OrderedQty == 5
		})).
		Return(nil).
		Once()

	got, err := svc.CreatePurchaseOrder(context.Background(), 7, 42, validCreatePurchaseOrderRequest())
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
}

func TestService_CreatePurchaseOrder_SupplierNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.NotFoundError("supplier not found")
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(5)).Return(nil, wantErr).Once()
	// Neither GetProduct nor CreatePurchaseOrder must be called for a
	// supplier that isn't ours.

	_, err := svc.CreatePurchaseOrder(context.Background(), 7, 42, validCreatePurchaseOrderRequest())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreatePurchaseOrder_ProductNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(5)).Return(&Supplier{ID: 5, OrgID: 7}, nil).Once()
	wantErr := common.NotFoundError("product not found")
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(nil, wantErr).Once()
	// CreatePurchaseOrder must never be called for a product that isn't
	// ours.

	_, err := svc.CreatePurchaseOrder(context.Background(), 7, 42, validCreatePurchaseOrderRequest())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreatePurchaseOrder_NonPositiveUnitCost_RejectsBeforeTouchingRepository(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(5)).Return(&Supplier{ID: 5, OrgID: 7}, nil).Once()
	// GetProduct/CreatePurchaseOrder must never be called - unit_cost fails
	// validation first.

	in := validCreatePurchaseOrderRequest()
	in.Items[0].UnitCost = decimal.Zero

	_, err := svc.CreatePurchaseOrder(context.Background(), 7, 42, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreatePurchaseOrder_DuplicatePoNumber_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(5)).Return(&Supplier{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&catalog.Product{ID: 2, OrgID: 7}, nil).Once()
	repo.EXPECT().
		CreatePurchaseOrder(mock.Anything, mock.Anything).
		Return(&pgconn.PgError{Code: "23505"}).
		Once()
	// CreatePurchaseOrderItems must never be called once the order insert
	// itself fails.

	_, err := svc.CreatePurchaseOrder(context.Background(), 7, 42, validCreatePurchaseOrderRequest())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreatePurchaseOrder_RepositoryError_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(5)).Return(&Supplier{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&catalog.Product{ID: 2, OrgID: 7}, nil).Once()
	dbErr := errors.New("connection refused")
	repo.EXPECT().CreatePurchaseOrder(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreatePurchaseOrder(context.Background(), 7, 42, validCreatePurchaseOrderRequest())
	require.ErrorIs(t, err, dbErr)
}

func TestService_GetPurchaseOrder_HappyPath_ReturnsOrderWithItems(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	items := []PurchaseOrderItem{{ID: 10, PoID: 1, ProductID: 1, OrderedQty: 10}}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	repo.EXPECT().ListPurchaseOrderItemsByPoID(mock.Anything, uint(1)).Return(items, nil).Once()

	got, err := svc.GetPurchaseOrder(context.Background(), 7, 1)
	require.NoError(t, err)
	require.Equal(t, uint(1), got.ID)
	require.Equal(t, items, got.Items)
}

func TestService_GetPurchaseOrder_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.NotFoundError("purchase order not found")
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.GetPurchaseOrder(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListPurchaseOrders_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	want := []PurchaseOrder{{ID: 1, SupplierID: 5}, {ID: 2, SupplierID: 6}}
	repo.EXPECT().ListPurchaseOrders(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListPurchaseOrders(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListPurchaseOrders_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListPurchaseOrders(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListPurchaseOrders(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_UpdatePurchaseOrder_HappyPath_UpdatesAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	updated := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusApproved}
	status := PurchaseOrderStatusApproved
	in := UpdatePurchaseOrderRequest{Status: &status}

	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().UpdatePurchaseOrder(mock.Anything, uint(1), map[string]any{"status": PurchaseOrderStatusApproved}).Return(nil).Once()
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(updated, nil).Once()

	got, err := svc.UpdatePurchaseOrder(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Same(t, updated, got)
}

func TestService_UpdatePurchaseOrder_NoFieldsProvided_SkipsWrite(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(existing, nil).Twice()
	// UpdatePurchaseOrder must never be called.

	got, err := svc.UpdatePurchaseOrder(context.Background(), 7, 1, UpdatePurchaseOrderRequest{})
	require.NoError(t, err)
	require.Same(t, existing, got)
}

func TestService_UpdatePurchaseOrder_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.NotFoundError("purchase order not found")
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.UpdatePurchaseOrder(context.Background(), 7, 999, UpdatePurchaseOrderRequest{})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdatePurchaseOrder_TerminalState_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusReceived}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	// UpdatePurchaseOrder must never be called - a received order is terminal.

	name := "PO-9999"
	_, err := svc.UpdatePurchaseOrder(context.Background(), 7, 1, UpdatePurchaseOrderRequest{PoNumber: &name})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_UpdatePurchaseOrder_RejectsStatusReceived_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	// UpdatePurchaseOrder must never be called - marking Received must go
	// through CreateGoodsReceipt instead.

	status := PurchaseOrderStatusReceived
	_, err := svc.UpdatePurchaseOrder(context.Background(), 7, 1, UpdatePurchaseOrderRequest{Status: &status})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_UpdatePurchaseOrder_NewSupplierNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	existing := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	wantErr := common.NotFoundError("supplier not found")
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(9)).Return(nil, wantErr).Once()
	// UpdatePurchaseOrder must never be called for a supplier that isn't ours.

	newSupplierID := uint(9)
	_, err := svc.UpdatePurchaseOrder(context.Background(), 7, 1, UpdatePurchaseOrderRequest{SupplierID: &newSupplierID})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func validCreateGoodsReceiptRequest() CreateGoodsReceiptRequest {
	return CreateGoodsReceiptRequest{
		ReceivedAt: time.Now(),
		Items: []CreateGoodsReceiptItemRequest{
			{PoItemID: 10, ReceivedQty: 10},
			{PoItemID: 11, ReceivedQty: 3, VarianceNote: "2 units damaged in transit"},
		},
	}
}

func TestService_CreateGoodsReceipt_HappyPath_CreditsStockAndLedgerAndMarksReceived(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	poItems := []PurchaseOrderItem{
		{ID: 10, PoID: 1, ProductID: 1, OrderedQty: 10, UnitCost: decimal.NewFromFloat(2.00)},
		{ID: 11, PoID: 1, ProductID: 2, OrderedQty: 5, UnitCost: decimal.NewFromFloat(1.00)},
	}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	repo.EXPECT().ListPurchaseOrderItemsByPoID(mock.Anything, uint(1)).Return(poItems, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, BranchID: 5}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(2)).Return(&catalog.Product{ID: 2, OrgID: 7, BranchID: 5}, nil).Once()

	repo.EXPECT().
		CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *GoodsReceipt) bool {
			return r.PoID == 1 && r.ReceivedBy == 42 && r.TotalAmount.Equal(decimal.NewFromFloat(23.00)) && r.VarianceCount == 2
		})).
		Run(func(_ *gorm.DB, r *GoodsReceipt) { r.ID = 100 }).
		Return(nil).
		Once()
	repo.EXPECT().
		CreateGoodsReceiptItems(mock.Anything, mock.MatchedBy(func(items []GoodsReceiptItem) bool {
			return len(items) == 2 && items[0].ReceiptID == 100 && items[0].PoItemID == 10 && items[0].ReceivedQty == 10 &&
				items[1].ReceiptID == 100 && items[1].PoItemID == 11 && items[1].ReceivedQty == 3 && items[1].VarianceNote == "2 units damaged in transit"
		})).
		Return(nil).
		Once()

	stock.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(nil, nil).Once()
	stock.EXPECT().
		CreateStockLevel(mock.Anything, mock.MatchedBy(func(l *inventory.StockLevel) bool {
			return l.ProductID == 1 && l.BranchID == 5 && l.Qty == 10
		})).
		Return(nil).
		Once()
	stock.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 20}, nil).Once()
	stock.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 23).Return(nil).Once()

	stock.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.OrgID == 7 && e.ProductID == 1 && e.BranchID == 5 && e.Type == inventory.LedgerEntryTypePurchaseReceipt &&
				e.Qty == 10 && e.BalanceAfter == 10 && e.ActorID != nil && *e.ActorID == 42 &&
				e.ReferenceType == inventory.ReferenceTypeGoodsReceipt && e.ReferenceID == "100"
		})).
		Return(nil).
		Once()
	stock.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.ProductID == 2 && e.BranchID == 5 && e.Qty == 3 && e.BalanceAfter == 23 && e.ReferenceID == "100"
		})).
		Return(nil).
		Once()

	repo.EXPECT().UpdatePurchaseOrderStatus(mock.Anything, uint(1), PurchaseOrderStatusReceived).Return(nil).Once()

	got, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, validCreateGoodsReceiptRequest())
	require.NoError(t, err)
	require.Equal(t, uint(100), got.ID)
}

func TestService_CreateGoodsReceipt_ZeroReceivedQty_NoStockOrLedgerMovement(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	poItems := []PurchaseOrderItem{{ID: 10, PoID: 1, ProductID: 1, OrderedQty: 10, UnitCost: decimal.NewFromFloat(2.00)}}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	repo.EXPECT().ListPurchaseOrderItemsByPoID(mock.Anything, uint(1)).Return(poItems, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, BranchID: 5}, nil).Once()
	repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *GoodsReceipt) { r.ID = 100 }).Return(nil).Once()
	repo.EXPECT().CreateGoodsReceiptItems(mock.Anything, mock.Anything).Return(nil).Once()
	// GetStockLevel/CreateStockLevel/UpdateStockLevelQty/
	// CreateInventoryLedgerEntry must never be called - nothing arrived for
	// this line.
	repo.EXPECT().UpdatePurchaseOrderStatus(mock.Anything, uint(1), PurchaseOrderStatusReceived).Return(nil).Once()

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, CreateGoodsReceiptRequest{
		ReceivedAt: time.Now(),
		Items:      []CreateGoodsReceiptItemRequest{{PoItemID: 10, ReceivedQty: 0, VarianceNote: "total loss - crushed in transit"}},
	})
	require.NoError(t, err)
}

func TestService_CreateGoodsReceipt_AlreadyReceived_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusReceived}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	// Nothing else must be called once the order is already received.

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, validCreateGoodsReceiptRequest())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateGoodsReceipt_Cancelled_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusCancelled}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, validCreateGoodsReceiptRequest())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateGoodsReceipt_UnknownPoItemID_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	poItems := []PurchaseOrderItem{{ID: 10, PoID: 1, ProductID: 1, OrderedQty: 10, UnitCost: decimal.NewFromFloat(2.00)}}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	repo.EXPECT().ListPurchaseOrderItemsByPoID(mock.Anything, uint(1)).Return(poItems, nil).Once()
	// Nothing else must be called - po_item_id 999 doesn't belong to this
	// order.

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, CreateGoodsReceiptRequest{
		ReceivedAt: time.Now(),
		Items:      []CreateGoodsReceiptItemRequest{{PoItemID: 999, ReceivedQty: 5}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateGoodsReceipt_NegativeReceivedQty_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	poItems := []PurchaseOrderItem{{ID: 10, PoID: 1, ProductID: 1, OrderedQty: 10, UnitCost: decimal.NewFromFloat(2.00)}}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	repo.EXPECT().ListPurchaseOrderItemsByPoID(mock.Anything, uint(1)).Return(poItems, nil).Once()

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, CreateGoodsReceiptRequest{
		ReceivedAt: time.Now(),
		Items:      []CreateGoodsReceiptItemRequest{{PoItemID: 10, ReceivedQty: -1}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateGoodsReceipt_MissingVarianceNoteOnShortfall_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	po := &PurchaseOrder{ID: 1, SupplierID: 5, Status: PurchaseOrderStatusSubmitted}
	poItems := []PurchaseOrderItem{{ID: 10, PoID: 1, ProductID: 1, OrderedQty: 10, UnitCost: decimal.NewFromFloat(2.00)}}
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(1)).Return(po, nil).Once()
	repo.EXPECT().ListPurchaseOrderItemsByPoID(mock.Anything, uint(1)).Return(poItems, nil).Once()
	// GetProduct/CreateGoodsReceipt must never be called - the missing
	// variance_note is caught first.

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 1, 42, CreateGoodsReceiptRequest{
		ReceivedAt: time.Now(),
		Items:      []CreateGoodsReceiptItemRequest{{PoItemID: 10, ReceivedQty: 8}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateGoodsReceipt_PurchaseOrderNotFound_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	stock := NewMockInventoryWriter(t)
	svc := NewService(repo, products, stock, fakeTransactioner{})

	wantErr := common.NotFoundError("purchase order not found")
	repo.EXPECT().GetPurchaseOrder(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.CreateGoodsReceipt(context.Background(), 7, 999, 42, validCreateGoodsReceiptRequest())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}
