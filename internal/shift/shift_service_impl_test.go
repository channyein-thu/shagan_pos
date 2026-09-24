package shift

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/common"
)

// openShiftRepositoryStub embeds Repository so this focused test double only
// needs to implement the operation under test. Any unexpected call to another
// repository method would panic through the nil embedded interface.
type openShiftRepositoryStub struct {
	Repository
	called           bool
	got              OpenShiftRequest
	result           *Shift
	err              error
	getCurrentCalled bool
	getCurrentOrgID  uint
	getCurrentUserID uint
	getCurrentResult *Shift
	getCurrentError  error
	getShiftCalled   bool
	getShiftScope    AccessScope
	getShiftID       uint
	getShiftResult   *Shift
	getShiftError    error
	closeShiftCalled bool
	closeShiftScope  AccessScope
	closeShiftID     uint
	closeShiftAt     time.Time
	closeShiftResult *Shift
	closeShiftError  error
}

func (r *openShiftRepositoryStub) OpenShift(_ context.Context, in OpenShiftRequest) (*Shift, error) {
	r.called = true
	r.got = in
	return r.result, r.err
}

func (r *openShiftRepositoryStub) GetCurrentShift(_ context.Context, orgID, userID uint) (*Shift, error) {
	r.getCurrentCalled = true
	r.getCurrentOrgID = orgID
	r.getCurrentUserID = userID
	return r.getCurrentResult, r.getCurrentError
}

func (r *openShiftRepositoryStub) GetShift(_ context.Context, scope AccessScope, id uint) (*Shift, error) {
	r.getShiftCalled = true
	r.getShiftScope = scope
	r.getShiftID = id
	return r.getShiftResult, r.getShiftError
}

func (r *openShiftRepositoryStub) CloseShift(_ context.Context, scope AccessScope, id uint, closedAt time.Time) (*Shift, error) {
	r.closeShiftCalled = true
	r.closeShiftScope = scope
	r.closeShiftID = id
	r.closeShiftAt = closedAt
	return r.closeShiftResult, r.closeShiftError
}

func TestService_OpenShift_NormalizesServerOwnedStateBeforePersisting(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 15, 9, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	closedAt := fixedNow.Add(-time.Hour)
	want := &Shift{ID: 42, Status: ShiftStatusOpen}
	repo := &openShiftRepositoryStub{result: want}
	svc := NewService(repo)
	svc.now = func() time.Time { return fixedNow }

	got, err := svc.OpenShift(context.Background(), OpenShiftRequest{
		BranchID:    1,
		StaffID:     2,
		DeviceID:    3,
		OpenedAt:    fixedNow.Add(-24 * time.Hour),
		OpeningCash: decimal.RequireFromString("150.25"),
		ClosedAt:    &closedAt,
		Status:      ShiftStatusClosed,
	})

	require.NoError(t, err)
	require.Same(t, want, got)
	require.True(t, repo.called)
	require.Equal(t, fixedNow.UTC(), repo.got.OpenedAt)
	require.Nil(t, repo.got.ClosedAt)
	require.Equal(t, ShiftStatusOpen, repo.got.Status)
	require.True(t, decimal.RequireFromString("150.25").Equal(repo.got.OpeningCash))
}

func TestService_OpenShift_RejectsInvalidInputWithoutPersisting(t *testing.T) {
	tests := []struct {
		name string
		in   OpenShiftRequest
	}{
		{name: "missing branch", in: validOpenShiftRequest(func(in *OpenShiftRequest) { in.BranchID = 0 })},
		{name: "missing staff", in: validOpenShiftRequest(func(in *OpenShiftRequest) { in.StaffID = 0 })},
		{name: "missing device", in: validOpenShiftRequest(func(in *OpenShiftRequest) { in.DeviceID = 0 })},
		{name: "negative opening cash", in: validOpenShiftRequest(func(in *OpenShiftRequest) {
			in.OpeningCash = decimal.RequireFromString("-0.01")
		})},
		{name: "fraction smaller than one cent", in: validOpenShiftRequest(func(in *OpenShiftRequest) {
			in.OpeningCash = decimal.RequireFromString("1.001")
		})},
		{name: "amount exceeds database precision", in: validOpenShiftRequest(func(in *OpenShiftRequest) {
			in.OpeningCash = decimal.RequireFromString("100000000.00")
		})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &openShiftRepositoryStub{}
			svc := NewService(repo)

			got, err := svc.OpenShift(context.Background(), tt.in)

			require.Nil(t, got)
			require.False(t, repo.called)
			requireRestErrorStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestService_OpenShift_PropagatesRepositoryError(t *testing.T) {
	wantErr := common.ConflictError("staff or device already has an open shift")
	repo := &openShiftRepositoryStub{err: wantErr}
	svc := NewService(repo)

	got, err := svc.OpenShift(context.Background(), validOpenShiftRequest(nil))

	require.Nil(t, got)
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr))
	require.Equal(t, wantErr, restErr)
}

func TestService_GetCurrentShift_UsesAuthenticatedScope(t *testing.T) {
	want := &Shift{ID: 42, BranchID: 7, DeviceID: 9, Status: ShiftStatusOpen}
	repo := &openShiftRepositoryStub{getCurrentResult: want}
	svc := NewService(repo)

	got, err := svc.GetCurrentShift(context.Background(), 3, 11)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.True(t, repo.getCurrentCalled)
	require.Equal(t, uint(3), repo.getCurrentOrgID)
	require.Equal(t, uint(11), repo.getCurrentUserID)
}

func TestService_GetCurrentShift_PropagatesRepositoryError(t *testing.T) {
	wantErr := common.NotFoundError("no open shift for current device")
	repo := &openShiftRepositoryStub{getCurrentError: wantErr}
	svc := NewService(repo)

	got, err := svc.GetCurrentShift(context.Background(), 3, 11)

	require.Nil(t, got)
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr))
	require.Equal(t, wantErr, restErr)
}

func TestService_GetShift_UsesAuthenticatedOrganization(t *testing.T) {
	want := &Shift{ID: 42, BranchID: 7, Status: ShiftStatusClosed}
	repo := &openShiftRepositoryStub{getShiftResult: want}
	svc := NewService(repo)

	scope := AccessScope{OrgID: 3}
	got, err := svc.GetShift(context.Background(), scope, 42)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.True(t, repo.getShiftCalled)
	require.Equal(t, scope, repo.getShiftScope)
	require.Equal(t, uint(42), repo.getShiftID)
}

func TestService_GetShift_PropagatesRepositoryError(t *testing.T) {
	wantErr := common.NotFoundError("shift not found")
	repo := &openShiftRepositoryStub{getShiftError: wantErr}
	svc := NewService(repo)

	got, err := svc.GetShift(context.Background(), AccessScope{OrgID: 3}, 999)

	require.Nil(t, got)
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr))
	require.Equal(t, wantErr, restErr)
}

func TestService_CloseShift_UsesAuthenticatedOrganizationAndServerTime(t *testing.T) {
	fixedNow := time.Date(2026, time.September, 15, 21, 45, 0, 0, time.FixedZone("ICT", 7*60*60))
	want := &Shift{ID: 42, Status: ShiftStatusClosed}
	repo := &openShiftRepositoryStub{closeShiftResult: want}
	svc := NewService(repo)
	svc.now = func() time.Time { return fixedNow }

	scope := AccessScope{OrgID: 3}
	got, err := svc.CloseShift(context.Background(), scope, 42)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.True(t, repo.closeShiftCalled)
	require.Equal(t, scope, repo.closeShiftScope)
	require.Equal(t, uint(42), repo.closeShiftID)
	require.Equal(t, fixedNow.UTC(), repo.closeShiftAt)
}

func TestService_CloseShift_PropagatesRepositoryError(t *testing.T) {
	wantErr := common.ConflictError("shift is already closed")
	repo := &openShiftRepositoryStub{closeShiftError: wantErr}
	svc := NewService(repo)

	got, err := svc.CloseShift(context.Background(), AccessScope{OrgID: 3}, 42)

	require.Nil(t, got)
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr))
	require.Equal(t, wantErr, restErr)
}

func validOpenShiftRequest(change func(*OpenShiftRequest)) OpenShiftRequest {
	in := OpenShiftRequest{
		BranchID:    1,
		StaffID:     2,
		DeviceID:    3,
		OpeningCash: decimal.RequireFromString("100.00"),
	}
	if change != nil {
		change(&in)
	}
	return in
}

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

type remainingRepositoryStub struct {
	Repository
	method          string
	scope           AccessScope
	id              uint
	drawerInput     CreateDrawerEventRequest
	createExpense   CreateExpenseRequest
	updateExpense   UpdateExpenseRequest
	shiftSummary    map[string]any
	reconciliations []ShiftReconciliation
	drawerEvent     *DrawerEvent
	drawerEvents    []DrawerEvent
	expense         *Expense
	expenses        []Expense
	err             error
}

func (r *remainingRepositoryStub) GetShiftSummary(_ context.Context, scope AccessScope, id uint) (map[string]any, error) {
	r.method, r.scope, r.id = "GetShiftSummary", scope, id
	return r.shiftSummary, r.err
}

func (r *remainingRepositoryStub) ListShiftReconciliations(_ context.Context, scope AccessScope, id uint) ([]ShiftReconciliation, error) {
	r.method, r.scope, r.id = "ListShiftReconciliations", scope, id
	return r.reconciliations, r.err
}

func (r *remainingRepositoryStub) CreateDrawerEvent(_ context.Context, scope AccessScope, in CreateDrawerEventRequest) (*DrawerEvent, error) {
	r.method, r.scope, r.drawerInput = "CreateDrawerEvent", scope, in
	return r.drawerEvent, r.err
}

func (r *remainingRepositoryStub) ListDrawerEvents(_ context.Context, scope AccessScope) ([]DrawerEvent, error) {
	r.method, r.scope = "ListDrawerEvents", scope
	return r.drawerEvents, r.err
}

func (r *remainingRepositoryStub) ListExpenses(_ context.Context, scope AccessScope) ([]Expense, error) {
	r.method, r.scope = "ListExpenses", scope
	return r.expenses, r.err
}

func (r *remainingRepositoryStub) CreateExpense(_ context.Context, scope AccessScope, in CreateExpenseRequest) (*Expense, error) {
	r.method, r.scope, r.createExpense = "CreateExpense", scope, in
	return r.expense, r.err
}

func (r *remainingRepositoryStub) UpdateExpense(_ context.Context, scope AccessScope, id uint, in UpdateExpenseRequest) (*Expense, error) {
	r.method, r.scope, r.id, r.updateExpense = "UpdateExpense", scope, id, in
	return r.expense, r.err
}

func (r *remainingRepositoryStub) DeleteExpense(_ context.Context, scope AccessScope, id uint) error {
	r.method, r.scope, r.id = "DeleteExpense", scope, id
	return r.err
}

func TestService_ShiftReadMethodsForwardAuthenticatedScope(t *testing.T) {
	branchID := uint(8)
	scope := AccessScope{OrgID: 7, BranchID: &branchID}

	t.Run("summary", func(t *testing.T) {
		want := map[string]any{"sales_count": int64(2)}
		repo := &remainingRepositoryStub{shiftSummary: want}
		got, err := NewService(repo).GetShiftSummary(context.Background(), scope, 42)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, "GetShiftSummary", repo.method)
		require.Equal(t, scope, repo.scope)
		require.Equal(t, uint(42), repo.id)
	})

	t.Run("reconciliations", func(t *testing.T) {
		want := []ShiftReconciliation{{ID: 1, ShiftID: 42}}
		repo := &remainingRepositoryStub{reconciliations: want}
		got, err := NewService(repo).ListShiftReconciliations(context.Background(), scope, 42)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, "ListShiftReconciliations", repo.method)
		require.Equal(t, scope, repo.scope)
	})
}

func TestService_CreateDrawerEvent_TrimsReasonAndPersists(t *testing.T) {
	scope := AccessScope{OrgID: 7}
	want := &DrawerEvent{ID: 1, ShiftID: 2, StaffID: 3, Reason: "cash count"}
	repo := &remainingRepositoryStub{drawerEvent: want}

	got, err := NewService(repo).CreateDrawerEvent(context.Background(), scope, CreateDrawerEventRequest{
		ShiftID: 2, StaffID: 3, Reason: "  cash count  ",
	})

	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, "CreateDrawerEvent", repo.method)
	require.Equal(t, "cash count", repo.drawerInput.Reason)
}

func TestService_CreateDrawerEvent_RejectsInvalidInput(t *testing.T) {
	zero := uint(0)
	tests := []CreateDrawerEventRequest{
		{StaffID: 1, Reason: "reason"},
		{ShiftID: 1, Reason: "reason"},
		{ShiftID: 1, StaffID: 1, Reason: "   "},
		{ShiftID: 1, StaffID: 1, Reason: "reason", SaleID: &zero},
	}
	for _, in := range tests {
		repo := &remainingRepositoryStub{}
		got, err := NewService(repo).CreateDrawerEvent(context.Background(), AccessScope{OrgID: 7}, in)
		require.Nil(t, got)
		require.Empty(t, repo.method)
		requireRestErrorStatus(t, err, http.StatusBadRequest)
	}
}

func TestService_ListDrawerEventsAndExpenses_ForwardScope(t *testing.T) {
	scope := AccessScope{OrgID: 7}
	t.Run("drawer events", func(t *testing.T) {
		want := []DrawerEvent{{ID: 1}}
		repo := &remainingRepositoryStub{drawerEvents: want}
		got, err := NewService(repo).ListDrawerEvents(context.Background(), scope)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, "ListDrawerEvents", repo.method)
		require.Equal(t, scope, repo.scope)
	})
	t.Run("expenses", func(t *testing.T) {
		want := []Expense{{ID: 1}}
		repo := &remainingRepositoryStub{expenses: want}
		got, err := NewService(repo).ListExpenses(context.Background(), scope)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, "ListExpenses", repo.method)
		require.Equal(t, scope, repo.scope)
	})
}

func TestService_CreateExpense_ValidatesNormalizesAndPersists(t *testing.T) {
	scope := AccessScope{OrgID: 7}
	want := &Expense{ID: 1}
	repo := &remainingRepositoryStub{expense: want}
	in := CreateExpenseRequest{
		BranchID: 2, Date: time.Now(), Category: "  supplies  ",
		Amount: decimal.RequireFromString("12.50"), CreatedBy: 3,
	}

	got, err := NewService(repo).CreateExpense(context.Background(), scope, in)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, "CreateExpense", repo.method)
	require.Equal(t, "supplies", repo.createExpense.Category)
	require.Equal(t, scope, repo.scope)
}

func TestService_CreateExpense_RejectsInvalidInput(t *testing.T) {
	tests := []CreateExpenseRequest{
		{Date: time.Now(), Category: "x", Amount: decimal.NewFromInt(1), CreatedBy: 1},
		{BranchID: 1, Category: "x", Amount: decimal.NewFromInt(1), CreatedBy: 1},
		{BranchID: 1, Date: time.Now(), Category: " ", Amount: decimal.NewFromInt(1), CreatedBy: 1},
		{BranchID: 1, Date: time.Now(), Category: "x", Amount: decimal.Zero, CreatedBy: 1},
		{BranchID: 1, Date: time.Now(), Category: "x", Amount: decimal.RequireFromString("1.001"), CreatedBy: 1},
		{BranchID: 1, Date: time.Now(), Category: "x", Amount: decimal.NewFromInt(1)},
	}
	for _, in := range tests {
		repo := &remainingRepositoryStub{}
		got, err := NewService(repo).CreateExpense(context.Background(), AccessScope{OrgID: 7}, in)
		require.Nil(t, got)
		require.Empty(t, repo.method)
		requireRestErrorStatus(t, err, http.StatusBadRequest)
	}
}

func TestService_UpdateExpense_ValidatesNormalizesAndPersists(t *testing.T) {
	category := "  transport  "
	amount := decimal.RequireFromString("20.25")
	scope := AccessScope{OrgID: 7}
	want := &Expense{ID: 9}
	repo := &remainingRepositoryStub{expense: want}

	got, err := NewService(repo).UpdateExpense(context.Background(), scope, 9, UpdateExpenseRequest{
		Category: &category, Amount: &amount,
	})

	require.NoError(t, err)
	require.Same(t, want, got)
	require.Equal(t, "UpdateExpense", repo.method)
	require.Equal(t, uint(9), repo.id)
	require.NotNil(t, repo.updateExpense.Category)
	require.Equal(t, "transport", *repo.updateExpense.Category)
}

func TestService_UpdateExpense_RejectsInvalidInput(t *testing.T) {
	zero := uint(0)
	empty := "   "
	zeroAmount := decimal.Zero
	zeroDate := time.Time{}
	tests := []UpdateExpenseRequest{
		{},
		{BranchID: &zero},
		{Date: &zeroDate},
		{Category: &empty},
		{Amount: &zeroAmount},
		{CreatedBy: &zero},
	}
	for _, in := range tests {
		repo := &remainingRepositoryStub{}
		got, err := NewService(repo).UpdateExpense(context.Background(), AccessScope{OrgID: 7}, 1, in)
		require.Nil(t, got)
		require.Empty(t, repo.method)
		requireRestErrorStatus(t, err, http.StatusBadRequest)
	}
}

func TestService_DeleteExpense_ForwardsScopeAndError(t *testing.T) {
	scope := AccessScope{OrgID: 7}
	wantErr := common.NotFoundError("expense not found")
	repo := &remainingRepositoryStub{err: wantErr}

	err := NewService(repo).DeleteExpense(context.Background(), scope, 9)

	var restErr common.RestError
	require.True(t, errors.As(err, &restErr))
	require.Equal(t, wantErr, restErr)
	require.Equal(t, "DeleteExpense", repo.method)
	require.Equal(t, uint(9), repo.id)
	require.Equal(t, scope, repo.scope)
}
