package inventory

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func TestService_ListStockLevels_OrgWide_ListsAllOrgBranches(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	want := []StockLevel{{ID: 1, ProductID: 1, BranchID: 5, Qty: 10}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()
	repo.EXPECT().ListStockLevels(mock.Anything, []uint{5, 6}, (*uint)(nil)).Return(want, nil).Once()

	got, err := svc.ListStockLevels(context.Background(), 7, nil, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListStockLevels_ScopedToOneBranch_VerifiesOwnershipFirst(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	branchID := uint(5)
	want := []StockLevel{{ID: 1, ProductID: 1, BranchID: 5, Qty: 10}}
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListStockLevels(mock.Anything, []uint{5}, (*uint)(nil)).Return(want, nil).Once()
	// ListBranches must never be called once a specific branch is given -
	// no .EXPECT() set up for it means the mock fails the test if it is.

	got, err := svc.ListStockLevels(context.Background(), 7, &branchID, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListStockLevels_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	branchID := uint(9)
	wantErr := common.NotFoundError("branch not found")
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(9)).Return(nil, wantErr).Once()
	// ListStockLevels must never be called for a branch that isn't ours -
	// no .EXPECT() set up for it means the mock fails the test if it is.

	_, err := svc.ListStockLevels(context.Background(), 7, &branchID, nil)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_ListStockLevels_FiltersByProductID(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	branchID := uint(5)
	productID := uint(3)
	want := []StockLevel{{ID: 1, ProductID: 3, BranchID: 5, Qty: 10}}
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListStockLevels(mock.Anything, []uint{5}, &productID).Return(want, nil).Once()

	got, err := svc.ListStockLevels(context.Background(), 7, &branchID, &productID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListStockLevels_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListStockLevels(mock.Anything, []uint{5}, (*uint)(nil)).Return(nil, wantErr).Once()

	_, err := svc.ListStockLevels(context.Background(), 7, nil, nil)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListStockLevels_ListBranchesFails_PropagatesAsIs(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	dbErr := errors.New("connection refused")
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(nil, dbErr).Once()
	// ListStockLevels must never be called once resolving the org's
	// branches itself fails.

	_, err := svc.ListStockLevels(context.Background(), 7, nil, nil)
	require.ErrorIs(t, err, dbErr)
}
