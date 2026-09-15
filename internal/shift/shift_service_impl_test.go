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
	getShiftOrgID    uint
	getShiftID       uint
	getShiftResult   *Shift
	getShiftError    error
	closeShiftCalled bool
	closeShiftOrgID  uint
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

func (r *openShiftRepositoryStub) GetShift(_ context.Context, orgID, id uint) (*Shift, error) {
	r.getShiftCalled = true
	r.getShiftOrgID = orgID
	r.getShiftID = id
	return r.getShiftResult, r.getShiftError
}

func (r *openShiftRepositoryStub) CloseShift(_ context.Context, orgID, id uint, closedAt time.Time) (*Shift, error) {
	r.closeShiftCalled = true
	r.closeShiftOrgID = orgID
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

	got, err := svc.GetShift(context.Background(), 3, 42)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.True(t, repo.getShiftCalled)
	require.Equal(t, uint(3), repo.getShiftOrgID)
	require.Equal(t, uint(42), repo.getShiftID)
}

func TestService_GetShift_PropagatesRepositoryError(t *testing.T) {
	wantErr := common.NotFoundError("shift not found")
	repo := &openShiftRepositoryStub{getShiftError: wantErr}
	svc := NewService(repo)

	got, err := svc.GetShift(context.Background(), 3, 999)

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

	got, err := svc.CloseShift(context.Background(), 3, 42)

	require.NoError(t, err)
	require.Same(t, want, got)
	require.True(t, repo.closeShiftCalled)
	require.Equal(t, uint(3), repo.closeShiftOrgID)
	require.Equal(t, uint(42), repo.closeShiftID)
	require.Equal(t, fixedNow.UTC(), repo.closeShiftAt)
}

func TestService_CloseShift_PropagatesRepositoryError(t *testing.T) {
	wantErr := common.ConflictError("shift is already closed")
	repo := &openShiftRepositoryStub{closeShiftError: wantErr}
	svc := NewService(repo)

	got, err := svc.CloseShift(context.Background(), 3, 42)

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
