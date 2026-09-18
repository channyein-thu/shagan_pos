package customer

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against a mocked Repository, which never
// dereferences the *gorm.DB it's handed (it just records the call). The
// real common.Transactioner is satisfied natively by *gorm.DB in production
// - same pattern as identity's own fakeTransactioner.
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

func TestService_ListCustomers_NoSearch_CallsListCustomers(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Customer{{ID: 1, OrgID: 7, Name: "Jane Doe"}, {ID: 2, OrgID: 7, Name: "John Smith"}}
	repo.EXPECT().ListCustomers(mock.Anything, uint(7)).Return(want, nil).Once()

	got, err := svc.ListCustomers(context.Background(), 7, "")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListCustomers_WithSearch_CallsSearchCustomers(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := []Customer{{ID: 1, OrgID: 7, Name: "Jane Doe"}}
	repo.EXPECT().SearchCustomers(mock.Anything, uint(7), "jane").Return(want, nil).Once()

	got, err := svc.ListCustomers(context.Background(), 7, "jane")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListCustomers_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListCustomers(mock.Anything, uint(7)).Return(nil, wantErr).Once()

	_, err := svc.ListCustomers(context.Background(), 7, "")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_CreateCustomer_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateCustomerRequest{Name: "Jane Doe", Phone: "0123456789"}
	want := &Customer{ID: 1, OrgID: 7, Name: "Jane Doe", Phone: "0123456789", ConsentStatus: ConsentStatusPending}
	repo.EXPECT().CreateCustomer(mock.Anything, uint(7), in).Return(want, nil).Once()

	got, err := svc.CreateCustomer(context.Background(), 7, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_CreateCustomer_PropagatesDuplicatePhoneConflict(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	in := CreateCustomerRequest{Name: "Jane Doe", Phone: "0123456789"}
	wantErr := common.ConflictError("a customer with this phone number already exists")
	repo.EXPECT().CreateCustomer(mock.Anything, uint(7), in).Return(nil, wantErr).Once()

	_, err := svc.CreateCustomer(context.Background(), 7, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_GetCustomer_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	want := &Customer{ID: 5, OrgID: 7, Name: "Jane Doe"}
	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(5)).Return(want, nil).Once()

	got, err := svc.GetCustomer(context.Background(), 7, 5)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_GetCustomer_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("customer not found")
	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()

	_, err := svc.GetCustomer(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListCustomerConsents_ChecksCustomerFirst_ThenDelegates(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	cust := &Customer{ID: 5, OrgID: 7}
	want := []CustomerConsent{{ID: 1, CustomerID: 5, Status: ConsentStatusGranted}}
	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(5)).Return(cust, nil).Once()
	repo.EXPECT().ListCustomerConsents(mock.Anything, uint(5)).Return(want, nil).Once()

	got, err := svc.ListCustomerConsents(context.Background(), 7, 5)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListCustomerConsents_PropagatesNotFoundFromGetCustomer(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("customer not found")
	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// ListCustomerConsents must never be called - no .EXPECT() set up for it
	// means the mock fails the test if it is.

	_, err := svc.ListCustomerConsents(context.Background(), 7, 999)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateCustomerConsent_HappyPath_CreatesConsentAndUpdatesCache(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	cust := &Customer{ID: 5, OrgID: 7}
	in := CreateCustomerConsentRequest{Status: ConsentStatusGranted, Source: ConsentSourcePos}
	want := &CustomerConsent{ID: 1, CustomerID: 5, Status: ConsentStatusGranted, Source: ConsentSourcePos}

	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(5)).Return(cust, nil).Once()
	repo.EXPECT().CreateCustomerConsent(mock.Anything, uint(5), in).Return(want, nil).Once()
	repo.EXPECT().UpdateCustomerConsentStatus(mock.Anything, uint(5), ConsentStatusGranted).Return(nil).Once()

	got, err := svc.CreateCustomerConsent(context.Background(), 7, 5, in)
	require.NoError(t, err)
	require.Same(t, want, got)
}

func TestService_CreateCustomerConsent_PropagatesNotFoundWhenCustomerMissing(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	wantErr := common.NotFoundError("customer not found")
	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(999)).Return(nil, wantErr).Once()
	// CreateCustomerConsent/UpdateCustomerConsentStatus must never be called.

	in := CreateCustomerConsentRequest{Status: ConsentStatusGranted, Source: ConsentSourcePos}
	_, err := svc.CreateCustomerConsent(context.Background(), 7, 999, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateCustomerConsent_StopsIfCreateConsentFails(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	cust := &Customer{ID: 5, OrgID: 7}
	in := CreateCustomerConsentRequest{Status: ConsentStatusGranted, Source: ConsentSourcePos}
	dbErr := errors.New("connection refused")

	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(5)).Return(cust, nil).Once()
	repo.EXPECT().CreateCustomerConsent(mock.Anything, uint(5), in).Return(nil, dbErr).Once()
	// UpdateCustomerConsentStatus must never be called if the consent insert failed.

	_, err := svc.CreateCustomerConsent(context.Background(), 7, 5, in)
	require.ErrorIs(t, err, dbErr)
}

func TestService_CreateCustomerConsent_PropagatesErrorFromCacheUpdate(t *testing.T) {
	repo := NewMockRepository(t)
	svc := newTestService(repo)

	cust := &Customer{ID: 5, OrgID: 7}
	in := CreateCustomerConsentRequest{Status: ConsentStatusGranted, Source: ConsentSourcePos}
	consent := &CustomerConsent{ID: 1, CustomerID: 5, Status: ConsentStatusGranted}
	dbErr := errors.New("connection refused")

	repo.EXPECT().GetCustomer(mock.Anything, uint(7), uint(5)).Return(cust, nil).Once()
	repo.EXPECT().CreateCustomerConsent(mock.Anything, uint(5), in).Return(consent, nil).Once()
	repo.EXPECT().UpdateCustomerConsentStatus(mock.Anything, uint(5), ConsentStatusGranted).Return(dbErr).Once()

	_, err := svc.CreateCustomerConsent(context.Background(), 7, 5, in)
	require.ErrorIs(t, err, dbErr)
}
