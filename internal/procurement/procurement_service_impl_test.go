package procurement

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/common"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func TestService_ListSuppliers_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	want := []Supplier{{ID: 1, OrgID: 7, Name: "Acme Foods"}, {ID: 2, OrgID: 7, Name: "Fresh Produce Co"}}
	repo.EXPECT().ListSuppliers(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListSuppliers(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListSuppliers_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListSuppliers(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListSuppliers(context.Background(), 7)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateSupplier_HappyPath_StampsLastOrderAtAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

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
	svc := NewService(repo)

	dbErr := errors.New("connection refused")
	repo.EXPECT().CreateSupplier(mock.Anything, mock.Anything).Return(dbErr).Once()

	_, err := svc.CreateSupplier(context.Background(), 7, CreateSupplierRequest{Name: "Acme Foods", Phone: "0123456789", Address: "123 Main St"})
	require.ErrorIs(t, err, dbErr)
}

func TestService_UpdateSupplier_HappyPath_UpdatesAndReturns(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

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
	svc := NewService(repo)

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
	svc := NewService(repo)

	wantErr := common.NotFoundError("supplier not found")
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// UpdateSupplier must never be called on a not-found supplier.

	_, err := svc.UpdateSupplier(context.Background(), 7, 999, UpdateSupplierRequest{})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_DeleteSupplier_HappyPath_Deletes(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

	existing := &Supplier{ID: 1, OrgID: 7, Name: "Acme Foods"}
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(1)).Return(existing, nil).Once()
	repo.EXPECT().PurchaseOrdersExistForSupplier(mock.Anything, uint(1)).Return(false, nil).Once()
	repo.EXPECT().DeleteSupplier(mock.Anything, uint(1)).Return(nil).Once()

	err := svc.DeleteSupplier(context.Background(), 7, 1)
	require.NoError(t, err)
}

func TestService_DeleteSupplier_InUseByPurchaseOrders_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo)

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
	svc := NewService(repo)

	wantErr := common.NotFoundError("supplier not found")
	repo.EXPECT().GetSupplier(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// Neither PurchaseOrdersExistForSupplier nor DeleteSupplier must be
	// called on a not-found supplier.

	err := svc.DeleteSupplier(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}
