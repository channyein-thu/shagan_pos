package inventory

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

// fakeTransactioner runs fc directly against a nil *gorm.DB, with no real
// database transaction - sufficient for unit tests that only exercise
// service-level orchestration against mocked dependencies. Same reasoning as
// procurement's equivalent; *gorm.DB satisfies common.Transactioner natively
// in production.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

func TestService_ListStockLevels_OrgWide_ListsAllOrgBranches(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

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
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

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
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

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
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

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
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

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
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	dbErr := errors.New("connection refused")
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(nil, dbErr).Once()
	// ListStockLevels must never be called once resolving the org's
	// branches itself fails.

	_, err := svc.ListStockLevels(context.Background(), 7, nil, nil)
	require.ErrorIs(t, err, dbErr)
}

func TestService_ListInventoryLedger_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	want := []InventoryLedger{{ID: 1, OrgID: 7, ProductID: 1, BranchID: 5, Qty: -2, BalanceAfter: 8}}
	repo.EXPECT().ListInventoryLedger(mock.Anything, uint(7), (*uint)(nil), (*uint)(nil)).Return(want, nil).Once()

	got, err := svc.ListInventoryLedger(context.Background(), 7, nil, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListInventoryLedger_FiltersByBranchAndProduct(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branchID := uint(5)
	productID := uint(3)
	want := []InventoryLedger{{ID: 1, OrgID: 7, ProductID: 3, BranchID: 5, Qty: -2, BalanceAfter: 8}}
	repo.EXPECT().ListInventoryLedger(mock.Anything, uint(7), &branchID, &productID).Return(want, nil).Once()
	// No BranchLookup call is needed - InventoryLedger carries its own
	// OrgID, unlike StockLevel.

	got, err := svc.ListInventoryLedger(context.Background(), 7, &branchID, &productID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListInventoryLedger_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().ListInventoryLedger(mock.Anything, uint(7), (*uint)(nil), (*uint)(nil)).Return(nil, wantErr).Once()

	_, err := svc.ListInventoryLedger(context.Background(), 7, nil, nil)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ListLowStock_OrgWide_ListsAllOrgBranches(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	want := []StockLevel{{ID: 1, ProductID: 1, BranchID: 5, Qty: 2}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()
	repo.EXPECT().ListLowStock(mock.Anything, []uint{5, 6}).Return(want, nil).Once()

	got, err := svc.ListLowStock(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListLowStock_ScopedToOneBranch_VerifiesOwnershipFirst(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branchID := uint(5)
	want := []StockLevel{{ID: 1, ProductID: 1, BranchID: 5, Qty: 2}}
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().ListLowStock(mock.Anything, []uint{5}).Return(want, nil).Once()

	got, err := svc.ListLowStock(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestService_ListLowStock_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branchID := uint(9)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(9)).Return(nil, common.NotFoundError("branch not found")).Once()
	// ListLowStock must never be called for a branch that isn't ours.

	_, err := svc.ListLowStock(context.Background(), 7, &branchID)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateStockAdjustment_HappyPath_ExistingStockLevel_CreditsAndLogsLedger(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 10}, nil).Once()
	repo.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 7).Return(nil).Once()
	repo.EXPECT().
		CreateStockAdjustment(mock.Anything, mock.MatchedBy(func(a *StockAdjustment) bool {
			return a.ProductID == 1 && a.BranchID == 5 && a.Delta == -3 && a.Reason == "damaged" && a.ActorID == 42
		})).
		Run(func(_ *gorm.DB, a *StockAdjustment) { a.ID = 100 }).
		Return(nil).
		Once()
	repo.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *InventoryLedger) bool {
			return e.OrgID == 7 && e.ProductID == 1 && e.BranchID == 5 && e.Type == LedgerEntryTypeAdjustment &&
				e.Qty == -3 && e.BalanceAfter == 7 && e.ActorID != nil && *e.ActorID == 42 &&
				e.ReferenceType == ReferenceTypeAdjustment && e.ReferenceID == "100"
		})).
		Return(nil).
		Once()

	got, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{BranchID: 5, ProductID: 1, Delta: -3, Reason: "damaged"})
	require.NoError(t, err)
	require.Equal(t, uint(100), got.ID)
}

func TestService_CreateStockAdjustment_NoExistingStockLevel_CreatesOne(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(nil, nil).Once()
	repo.EXPECT().
		CreateStockLevel(mock.Anything, mock.MatchedBy(func(l *StockLevel) bool {
			return l.ProductID == 1 && l.BranchID == 5 && l.Qty == 20
		})).
		Return(nil).
		Once()
	repo.EXPECT().CreateStockAdjustment(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{BranchID: 5, ProductID: 1, Delta: 20, Reason: "initial count"})
	require.NoError(t, err)
}

func TestService_CreateStockAdjustment_InsufficientStock_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 2}, nil).Once()
	// The adjustment supplies the ledger reference ID before movement. Its
	// insert rolls back with the transaction when stock is insufficient.
	repo.EXPECT().CreateStockAdjustment(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{BranchID: 5, ProductID: 1, Delta: -5, Reason: "damaged"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateStockAdjustment_UnknownProduct_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(99)).Return(nil, common.NotFoundError("product not found")).Once()
	// GetStockLevel must never be called - the product lookup failed first.

	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{BranchID: 5, ProductID: 99, Delta: 5, Reason: "found extra"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateStockAdjustment_WithUnitCost_BlendsWeightedAverageCost(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	unitCost := d("3.00")
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7, CostPrice: d("1.00")}, nil).Once()
	// existing 10 units @ 1.00 blended with 10 more @ 3.00: (10*1 + 10*3)/20 = 2.00
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 10}, nil).Once()
	repo.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 20).Return(nil).Once()
	products.EXPECT().
		UpdateProduct(mock.Anything, uint(1), mock.MatchedBy(func(updates map[string]any) bool {
			cost, ok := updates["cost_price"].(decimal.Decimal)
			return ok && cost.Equal(d("2.00"))
		})).
		Return(nil).Once()
	repo.EXPECT().CreateStockAdjustment(mock.Anything, mock.Anything).Return(nil).Once()
	repo.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{
		BranchID: 5, ProductID: 1, Delta: 10, Reason: "restocked", UnitCost: &unitCost,
	})
	require.NoError(t, err)
}

func TestService_CreateStockAdjustment_BranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(9)).Return(nil, common.NotFoundError("branch not found")).Once()
	// GetProduct/GetStockLevel must never be called - the branch isn't ours.

	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{BranchID: 9, ProductID: 1, Delta: 5, Reason: "found extra"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateStockAdjustment_UnitCostWithNonPositiveDelta_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})
	// GetProduct must never be called - rejected before any lookup.

	unitCost := d("3.00")
	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{
		ProductID: 1, Delta: -5, Reason: "shrinkage", UnitCost: &unitCost,
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateStockAdjustment_NegativeUnitCost_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	unitCost := d("-1.00")
	_, err := svc.CreateStockAdjustment(context.Background(), 7, 42, CreateStockAdjustmentRequest{
		ProductID: 1, Delta: 5, Reason: "found extra", UnitCost: &unitCost,
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_ListStockTransfers_OrgWide_ListsAllOrgBranches(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	transfers := []StockTransfer{
		{ID: 1, FromBranch: 5, ToBranch: 6, Status: TransferStatusPending, Note: "restock"},
		{ID: 2, FromBranch: 6, ToBranch: 5, Status: TransferStatusCompleted},
		{ID: 3, FromBranch: 5, ToBranch: 6, Status: TransferStatusPending},
	}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()
	repo.EXPECT().ListStockTransfers(mock.Anything, []uint{5, 6}).Return(transfers, nil).Once()
	// One query for every transfer on the list, not one per transfer.
	repo.EXPECT().ListStockTransferItemsByTransferIDs(mock.Anything, []uint{1, 2, 3}).Return([]StockTransferItem{
		{ID: 10, TransferID: 1, ProductID: 8, Qty: 4},
		{ID: 11, TransferID: 1, ProductID: 9, Qty: 1},
		{ID: 12, TransferID: 2, ProductID: 8, Qty: 2},
	}, nil).Once()

	got, err := svc.ListStockTransfers(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, uint(1), got[0].ID)
	require.Equal(t, "restock", got[0].Note)
	require.Equal(t, []StockTransferItemResult{{ProductID: 8, Qty: 4}, {ProductID: 9, Qty: 1}}, got[0].Items)
	require.Equal(t, []StockTransferItemResult{{ProductID: 8, Qty: 2}}, got[1].Items)
	require.Equal(t, []StockTransferItemResult{}, got[2].Items, "never null")
}

func TestService_ListStockTransfers_NoTransfers_SkipsItemQueryAndReturnsEmpty(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, NewMockProductLookup(t), fakeTransactioner{})

	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	repo.EXPECT().ListStockTransfers(mock.Anything, []uint{5}).Return(nil, nil).Once()

	got, err := svc.ListStockTransfers(context.Background(), 7, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestService_ListStockTransfers_ItemLookupFails_Propagates(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches, NewMockProductLookup(t), fakeTransactioner{})

	boom := errors.New("db down")
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	repo.EXPECT().ListStockTransfers(mock.Anything, []uint{5}).Return([]StockTransfer{{ID: 1}}, nil).Once()
	repo.EXPECT().ListStockTransferItemsByTransferIDs(mock.Anything, []uint{1}).Return(nil, boom).Once()

	_, err := svc.ListStockTransfers(context.Background(), 7, nil)
	require.ErrorIs(t, err, boom)
}

func TestService_CreateStockTransfer_HappyPath_CreatesPendingTransferWithItems(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(6)).Return(&identity.Branch{ID: 6, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 10}, nil).Once()

	repo.EXPECT().
		CreateStockTransfer(mock.Anything, mock.MatchedBy(func(tr *StockTransfer) bool {
			return tr.FromBranch == 5 && tr.ToBranch == 6 && tr.Status == TransferStatusPending && tr.ActorID == 42 && tr.Note == "restock for weekend"
		})).
		Run(func(_ *gorm.DB, tr *StockTransfer) { tr.ID = 100 }).
		Return(nil).
		Once()
	repo.EXPECT().
		CreateStockTransferItems(mock.Anything, mock.MatchedBy(func(items []StockTransferItem) bool {
			return len(items) == 1 && items[0].TransferID == 100 && items[0].ProductID == 1 && items[0].Qty == 4
		})).
		Return(nil).
		Once()

	in := CreateStockTransferRequest{
		FromBranch: 5, ToBranch: 6, Note: "  restock for weekend  ",
		Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}},
	}
	got, err := svc.CreateStockTransfer(context.Background(), 7, 42, in)
	require.NoError(t, err)
	require.Equal(t, uint(100), got.ID)
	require.Equal(t, TransferStatusPending, got.Status)
	require.Equal(t, "restock for weekend", got.Note, "stored and returned trimmed")
	require.Equal(t, []StockTransferItemResult{{ProductID: 1, Qty: 4}}, got.Items)
}

func TestService_CreateStockTransfer_NoNote_StoresEmptyString(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(6)).Return(&identity.Branch{ID: 6, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 10}, nil).Once()
	repo.EXPECT().CreateStockTransfer(mock.Anything, mock.MatchedBy(func(tr *StockTransfer) bool { return tr.Note == "" })).
		Run(func(_ *gorm.DB, tr *StockTransfer) { tr.ID = 1 }).Return(nil).Once()
	repo.EXPECT().CreateStockTransferItems(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := svc.CreateStockTransfer(context.Background(), 7, 42, CreateStockTransferRequest{
		FromBranch: 5, ToBranch: 6, Note: "   ",
		Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}},
	})
	require.NoError(t, err)
	require.Equal(t, "", got.Note)
}

func TestService_CreateStockTransfer_NoteTooLong_RejectedBeforeAnyLookup(t *testing.T) {
	repo := NewMockRepository(t)
	svc := NewService(repo, NewMockBranchLookup(t), NewMockProductLookup(t), fakeTransactioner{})

	_, err := svc.CreateStockTransfer(context.Background(), 7, 42, CreateStockTransferRequest{
		FromBranch: 5, ToBranch: 6, Note: strings.Repeat("x", 501),
		Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}},
	})
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateStockTransfer_SameFromAndToBranch_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})
	// GetBranch/GetProduct must never be called - rejected before any lookup.

	in := CreateStockTransferRequest{FromBranch: 5, ToBranch: 5, Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}}}
	_, err := svc.CreateStockTransfer(context.Background(), 7, 42, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateStockTransfer_InsufficientStockAtFromBranch_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(6)).Return(&identity.Branch{ID: 6, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 2}, nil).Once()
	// CreateStockTransfer must never be called - rejected before any write.

	in := CreateStockTransferRequest{FromBranch: 5, ToBranch: 6, Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}}}
	_, err := svc.CreateStockTransfer(context.Background(), 7, 42, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateStockTransfer_NoExistingStockLevelAtFromBranch_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(6)).Return(&identity.Branch{ID: 6, OrgID: 7}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(&catalog.Product{ID: 1, OrgID: 7}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(nil, nil).Once()
	// CreateStockTransfer must never be called - rejected before any write.

	in := CreateStockTransferRequest{FromBranch: 5, ToBranch: 6, Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}}}
	_, err := svc.CreateStockTransfer(context.Background(), 7, 42, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateStockTransfer_ToBranchNotInOrg_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(9)).Return(nil, common.NotFoundError("branch not found")).Once()
	// GetProduct/CreateStockTransfer must never be called.

	in := CreateStockTransferRequest{FromBranch: 5, ToBranch: 9, Items: []CreateStockTransferItemRequest{{ProductID: 1, Qty: 4}}}
	_, err := svc.CreateStockTransfer(context.Background(), 7, 42, in)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_UpdateStockTransfer_CompletingTransfer_MovesStockAtBothBranchesAndLogsLedger(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()

	transfer := &StockTransfer{ID: 1, FromBranch: 5, ToBranch: 6, Status: TransferStatusInTransit, ActorID: 42}
	repo.EXPECT().GetStockTransfer(mock.Anything, []uint{5, 6}, uint(1), true).Return(transfer, nil).Once()
	repo.EXPECT().ListStockTransferItems(mock.Anything, uint(1)).Return([]StockTransferItem{{ID: 1, TransferID: 1, ProductID: 1, Qty: 4}}, nil).Once()

	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 10}, nil).Once()
	repo.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 6).Return(nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(6)).Return(nil, nil).Once()
	repo.EXPECT().
		CreateStockLevel(mock.Anything, mock.MatchedBy(func(l *StockLevel) bool {
			return l.ProductID == 1 && l.BranchID == 6 && l.Qty == 4
		})).
		Return(nil).
		Once()

	repo.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *InventoryLedger) bool {
			return e.ProductID == 1 && e.BranchID == 5 && e.Type == LedgerEntryTypeTransferOut &&
				e.Qty == -4 && e.BalanceAfter == 6 && e.ActorID != nil && *e.ActorID == 42 &&
				e.ReferenceType == ReferenceTypeStockTransfer && e.ReferenceID == "1"
		})).
		Return(nil).
		Once()
	repo.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *InventoryLedger) bool {
			return e.ProductID == 1 && e.BranchID == 6 && e.Type == LedgerEntryTypeTransferIn &&
				e.Qty == 4 && e.BalanceAfter == 4 && e.ReferenceID == "1"
		})).
		Return(nil).
		Once()

	repo.EXPECT().UpdateStockTransferStatus(mock.Anything, uint(1), TransferStatusCompleted).Return(nil).Once()

	got, err := svc.UpdateStockTransfer(context.Background(), 7, 1, UpdateStockTransferRequest{Status: TransferStatusCompleted})
	require.NoError(t, err)
	require.Equal(t, TransferStatusCompleted, got.Status)
	require.Equal(t, []StockTransferItemResult{{ProductID: 1, Qty: 4}}, got.Items, "the completed transfer's response still shows what moved")
}

func TestService_UpdateStockTransfer_CompletingWithInsufficientFromStock_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()

	transfer := &StockTransfer{ID: 1, FromBranch: 5, ToBranch: 6, Status: TransferStatusPending, ActorID: 42}
	repo.EXPECT().GetStockTransfer(mock.Anything, []uint{5, 6}, uint(1), true).Return(transfer, nil).Once()
	repo.EXPECT().ListStockTransferItems(mock.Anything, uint(1)).Return([]StockTransferItem{{ID: 1, TransferID: 1, ProductID: 1, Qty: 4}}, nil).Once()
	repo.EXPECT().GetStockLevel(mock.Anything, uint(1), uint(5)).Return(&StockLevel{ID: 50, ProductID: 1, BranchID: 5, Qty: 1}, nil).Once()
	// UpdateStockTransferStatus must never be called - the movement is
	// rejected before the status write happens.

	_, err := svc.UpdateStockTransfer(context.Background(), 7, 1, UpdateStockTransferRequest{Status: TransferStatusCompleted})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_UpdateStockTransfer_AlreadyTerminal_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()

	transfer := &StockTransfer{ID: 1, FromBranch: 5, ToBranch: 6, Status: TransferStatusCompleted, ActorID: 42}
	repo.EXPECT().GetStockTransfer(mock.Anything, []uint{5, 6}, uint(1), true).Return(transfer, nil).Once()
	// ListStockTransferItems/UpdateStockTransferStatus must never be called -
	// rejected before either happens.

	_, err := svc.UpdateStockTransfer(context.Background(), 7, 1, UpdateStockTransferRequest{Status: TransferStatusCancelled})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_UpdateStockTransfer_Cancelling_NoStockMovement(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()

	transfer := &StockTransfer{ID: 1, FromBranch: 5, ToBranch: 6, Status: TransferStatusPending, ActorID: 42}
	repo.EXPECT().GetStockTransfer(mock.Anything, []uint{5, 6}, uint(1), true).Return(transfer, nil).Once()
	// The lines are read so the response can carry them, but
	// GetStockLevel/CreateInventoryLedgerEntry must never be called -
	// cancelling moves no stock.
	repo.EXPECT().ListStockTransferItems(mock.Anything, uint(1)).Return([]StockTransferItem{{ID: 10, TransferID: 1, ProductID: 8, Qty: 3}}, nil).Once()
	repo.EXPECT().UpdateStockTransferStatus(mock.Anything, uint(1), TransferStatusCancelled).Return(nil).Once()

	got, err := svc.UpdateStockTransfer(context.Background(), 7, 1, UpdateStockTransferRequest{Status: TransferStatusCancelled})
	require.NoError(t, err)
	require.Equal(t, TransferStatusCancelled, got.Status)
	require.Equal(t, []StockTransferItemResult{{ProductID: 8, Qty: 3}}, got.Items)
}

func TestService_UpdateStockTransfer_UnknownID_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	svc := NewService(repo, branches, products, fakeTransactioner{})

	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	repo.EXPECT().GetStockTransfer(mock.Anything, []uint{5}, uint(99), true).Return(nil, common.NotFoundError("stock transfer not found")).Once()

	_, err := svc.UpdateStockTransfer(context.Background(), 7, 99, UpdateStockTransferRequest{Status: TransferStatusCancelled})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotFound)
}
