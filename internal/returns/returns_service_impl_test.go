package returns

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

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/sales"
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
// database transaction - same reasoning as procurement's/inventory's own
// equivalents.
type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

func newTestService(repo Repository, branches BranchLookup, salesRepo SalesReader, inv InventoryWriter, auditWriter AuditWriter) *Service {
	return NewService(repo, branches, salesRepo, nil, inv, auditWriter, fakeTransactioner{})
}

// --- VoidSale ---

func TestService_VoidSale_HappyPath_CreditsStockAndMarksSaleVoided(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 3}}

	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(5), uint(9)).Return(nil).Once()
	repo.EXPECT().SaleHasReturnOrExchange(mock.Anything, saleID).Return(false, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().
		CreateVoid(mock.Anything, mock.MatchedBy(func(v *Void) bool {
			return v.SaleID == saleID && v.Qty == 3 && v.Reason == VoidReasonStaffError && v.Explanation == "rang up wrong item" && v.ApprovedBy != nil && *v.ApprovedBy == 30 && v.ApprovedByUserID == nil
		})).
		Run(func(_ *gorm.DB, v *Void) { v.ID = 100 }).
		Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 4}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 7).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.OrgID == 7 && e.ProductID == 2 && e.BranchID == 5 && e.Type == inventory.LedgerEntryTypeVoid &&
				e.Qty == 3 && e.BalanceAfter == 7 && e.ActorID != nil && *e.ActorID == 30 &&
				e.ReferenceType == inventory.ReferenceTypeVoid && e.ReferenceID == "100"
		})).
		Return(nil).Once()
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, sales.SaleStatusVoided).Return(nil).Once()
	audW.EXPECT().
		CreateAuditLog(mock.Anything, mock.MatchedBy(func(e *audit.AuditLog) bool {
			return e.OrgID == 7 && e.ActorID != nil && *e.ActorID == 30 && e.BranchID != nil && *e.BranchID == 5 &&
				e.Entity == "sale" && e.EntityID == saleID.String() && e.Action == "voided"
		})).
		Return(nil).Once()

	got, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonStaffError, Explanation: "rang up wrong item"})
	require.NoError(t, err)
	require.Equal(t, uint(100), got.ID)
}

// The till's void screen sends one of its own reason codes and no explanation;
// both must be stored as given ("" is a valid explanation, not NULL).
func TestService_VoidSale_TillReasonWithoutExplanation_IsRecorded(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(5), uint(9)).Return(nil).Once()
	repo.EXPECT().SaleHasReturnOrExchange(mock.Anything, saleID).Return(false, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{}, nil).Once()
	repo.EXPECT().
		CreateVoid(mock.Anything, mock.MatchedBy(func(v *Void) bool {
			return v.Reason == VoidReasonDuplicateTransaction && v.Explanation == ""
		})).
		Return(nil).Once()
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, sales.SaleStatusVoided).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonDuplicateTransaction})

	require.NoError(t, err)
}

// An Owner voiding directly has no Staff record: recorded as approved_by_user_id,
// audit actor_user_id, and no (fake) staff actor on the ledger rows.
func TestService_VoidSale_ByOwner_RecordsUserNotStaff(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 3}}

	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	// The "shift must still be open" rule applies to the Owner too.
	salesRepo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(5), uint(9)).Return(nil).Once()
	repo.EXPECT().SaleHasReturnOrExchange(mock.Anything, saleID).Return(false, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().
		CreateVoid(mock.Anything, mock.MatchedBy(func(v *Void) bool {
			return v.ApprovedBy == nil && v.ApprovedByUserID != nil && *v.ApprovedByUserID == 77
		})).
		Run(func(_ *gorm.DB, v *Void) { v.ID = 100 }).
		Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 4}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 7).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.Type == inventory.LedgerEntryTypeVoid && e.ActorID == nil && e.ReferenceID == "100"
		})).
		Return(nil).Once()
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, sales.SaleStatusVoided).Return(nil).Once()
	audW.EXPECT().
		CreateAuditLog(mock.Anything, mock.MatchedBy(func(e *audit.AuditLog) bool {
			return e.ActorID == nil && e.ActorUserID != nil && *e.ActorUserID == 77 && e.Action == "voided"
		})).
		Return(nil).Once()

	got, err := svc.VoidSale(context.Background(), 7, Actor{UserID: 77, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonStaffError, Explanation: "owner correction"})
	require.NoError(t, err)
	require.Nil(t, got.ApprovedBy)
	require.Equal(t, uint(77), *got.ApprovedByUserID)
}

func TestService_VoidSale_AlreadyHasReturnOrExchange_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(5), uint(9)).Return(nil).Once()
	repo.EXPECT().SaleHasReturnOrExchange(mock.Anything, saleID).Return(true, nil).Once()
	// ListSaleItemsTx/CreateVoid must never be called - crediting back the
	// full original qty would double-credit what a prior Return/Exchange
	// already credited.

	_, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonOther, Explanation: "test"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_VoidSale_NoPermission_ReturnsForbidden(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	// No GetSaleWithLock call - rejected before any lookup.

	_, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: false}, uuid.New(),
		VoidSaleRequest{Reason: VoidReasonOther, Explanation: "test"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusForbidden)
}

func TestService_VoidSale_AlreadyVoided_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusVoided}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	// RequireOpenShift/ListSaleItemsTx/CreateVoid must never be called.

	_, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonOther, Explanation: "test"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_VoidSale_ShiftNoLongerOpen_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().RequireOpenShift(mock.Anything, uint(7), uint(5), uint(9)).
		Return(common.NotFoundError("shift not found, not open, or doesn't belong to this branch")).Once()
	// ListSaleItemsTx/CreateVoid must never be called.

	_, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonOther, Explanation: "test"})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_ListVoids_OrgWide_ListsAllOrgBranches(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	want := []Void{{ID: 1, SaleID: uuid.New()}}
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()
	repo.EXPECT().ListVoids(mock.Anything, []uint{5, 6}).Return(want, nil).Once()

	got, err := svc.ListVoids(context.Background(), 7, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// --- CreateReturn ---

func TestService_CreateReturn_HappyPath_SellableItemRestocksAndComputesRefund(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}

	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().
		CreateReturn(mock.Anything, mock.MatchedBy(func(r *Return) bool {
			return r.SaleID == saleID && r.ReasonCode == ReturnReasonCodeDefective && r.RefundMethod == RefundMethodCash &&
				r.RefundTotal.Equal(d("20.00")) && r.ApprovedBy == 30
		})).
		Run(func(_ *gorm.DB, r *Return) { r.ID = 200 }).
		Return(nil).Once()
	repo.EXPECT().
		CreateReturnItems(mock.Anything, mock.MatchedBy(func(items []ReturnItem) bool {
			return len(items) == 1 && items[0].ReturnID == 200 && items[0].SaleItemID == 1 && items[0].Qty == 2 &&
				items[0].Condition == ItemConditionSellable && items[0].Restocked
		})).
		Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 1}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 3).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.Type == inventory.LedgerEntryTypeReturn && e.Qty == 2 && e.BalanceAfter == 3 &&
				e.ReferenceType == inventory.ReferenceTypeReturn && e.ReferenceID == "200"
		})).
		Return(nil).Once()
	audW.EXPECT().
		CreateAuditLog(mock.Anything, mock.MatchedBy(func(e *audit.AuditLog) bool {
			return e.OrgID == 7 && e.ActorID != nil && *e.ActorID == 30 && e.BranchID != nil && *e.BranchID == 5 &&
				e.Entity == "sale" && e.EntityID == saleID.String() && e.Action == "returned"
		})).
		Return(nil).Once()

	in := CreateReturnRequest{
		SaleID:       saleID,
		Items:        []CreateReturnItemRequest{{SaleItemID: 1, Qty: 2, Condition: ItemConditionSellable}},
		ReasonCode:   ReturnReasonCodeDefective,
		RefundMethod: RefundMethodCash,
	}
	got, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, in)
	require.NoError(t, err)
	require.Equal(t, uint(200), got.ID)
}

func TestService_CreateReturn_NonSellableCondition_NotRestocked(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}

	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Return) { r.ID = 201 }).Return(nil).Once()
	repo.EXPECT().
		CreateReturnItems(mock.Anything, mock.MatchedBy(func(items []ReturnItem) bool {
			return len(items) == 1 && !items[0].Restocked
		})).
		Return(nil).Once()
	// A damaged item isn't restocked: its stock is never changed
	// (UpdateStockLevelQty is never called), but the write-off is recorded.
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 12}, nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.Type == inventory.LedgerEntryTypeReturnWriteoff && e.Qty == 0 && e.BalanceAfter == 12
		})).
		Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	in := CreateReturnRequest{
		SaleID:       saleID,
		Items:        []CreateReturnItemRequest{{SaleItemID: 1, Qty: 1, Condition: ItemConditionDamaged}},
		ReasonCode:   ReturnReasonCodeDefective,
		RefundMethod: RefundMethodQR,
	}
	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, in)
	require.NoError(t, err)
}

// returnOneLineWithCondition runs a 1-line return of the given condition
// against a fresh fixture and reports the Return and ReturnItem rows the
// service handed to the repository. UpdateStockLevelQty is never expected, so
// a restock attempt would fail the test as an unexpected mock call.
func returnOneLineWithCondition(t *testing.T, condition ItemCondition, explanation string) (Return, ReturnItem) {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	var savedReturn Return
	var savedItem ReturnItem
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, r *Return) { r.ID = 300; savedReturn = *r }).Return(nil).Once()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, items []ReturnItem) { savedItem = items[0] }).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 3}, nil).Once()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID:       saleID,
		Items:        []CreateReturnItemRequest{{SaleItemID: 1, Qty: 1, Condition: condition}},
		ReasonCode:   ReturnReasonCodeOther,
		Explanation:  explanation,
		RefundMethod: RefundMethodQR,
	})
	require.NoError(t, err)
	return savedReturn, savedItem
}

// The till makes the cashier type why the customer is returning the goods;
// that text was being lost because Return had nowhere to keep it.
func TestService_CreateReturn_Explanation_IsPersisted(t *testing.T) {
	ret, _ := returnOneLineWithCondition(t, ItemConditionDamaged, "bottle was leaking")
	require.Equal(t, "bottle was leaking", ret.Explanation)
}

func TestService_CreateReturn_NoExplanation_StoresEmptyString(t *testing.T) {
	ret, _ := returnOneLineWithCondition(t, ItemConditionDamaged, "")
	require.Equal(t, "", ret.Explanation)
}

// The till's Expired / Other conditions are stored as given, not mapped onto
// the older values, and only sellable ever goes back on the shelf.
func TestService_CreateReturn_ExpiredAndOtherConditions_StoredAndNotRestocked(t *testing.T) {
	for _, condition := range []ItemCondition{ItemConditionExpired, ItemConditionOther} {
		t.Run(string(condition), func(t *testing.T) {
			_, item := returnOneLineWithCondition(t, condition, "")
			require.Equal(t, condition, item.Condition)
			require.False(t, item.Restocked)
		})
	}
}

// The older condition values keep working for existing clients.
func TestService_CreateReturn_OldConditions_StillStoredAndNotRestocked(t *testing.T) {
	for _, condition := range []ItemCondition{ItemConditionDamaged, ItemConditionOpened, ItemConditionDefective} {
		t.Run(string(condition), func(t *testing.T) {
			_, item := returnOneLineWithCondition(t, condition, "")
			require.Equal(t, condition, item.Condition)
			require.False(t, item.Restocked)
		})
	}
}

// A non-restocked return changes no stock, so without a row of its own it
// would show nowhere in the owner's Inventory Ledger. The row has qty 0 (stock
// didn't move) and the balance as it stands.
func TestService_CreateReturn_DefectiveLine_WritesOffLedgerEntryWithZeroQty(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Return) { r.ID = 400 }).Return(nil).Once()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 12}, nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.OrgID == 7 && e.ProductID == 2 && e.BranchID == 5 &&
				e.Type == inventory.LedgerEntryTypeReturnWriteoff && e.Qty == 0 && e.BalanceAfter == 12 &&
				e.ActorID != nil && *e.ActorID == 30 &&
				e.ReferenceType == inventory.ReferenceTypeReturn && e.ReferenceID == "400"
		})).
		Return(nil).Once() // exactly one row, and no UpdateStockLevelQty expectation: stock is untouched
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID:       saleID,
		Items:        []CreateReturnItemRequest{{SaleItemID: 1, Qty: 2, Condition: ItemConditionDefective}},
		ReasonCode:   ReturnReasonCodeDefective,
		RefundMethod: RefundMethodCash,
	})
	require.NoError(t, err)
}

// One return can mix a sellable line (restocked, normal "return" row) and a
// non-sellable one (write-off row) - each gets its own kind of entry.
func TestService_CreateReturn_MixedLines_WriteNormalAndWriteoffEntries(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{
		{ID: 1, SaleID: saleID, ProductID: 2, Qty: 2, LineTotal: d("20.00")},
		{ID: 2, SaleID: saleID, ProductID: 3, Qty: 1, LineTotal: d("5.00")},
	}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, mock.Anything).Return(0, nil).Twice()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, mock.Anything).Return(0, nil).Twice()
	// Both lines come back in full, so the sale becomes refunded.
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, sales.SaleStatusRefunded).Return(nil).Once()
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Return) { r.ID = 401 }).Return(nil).Once()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
	// product 2: sellable, 5 -> 7. product 3: expired, balance stays 9.
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 5}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 7).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(3), uint(5)).Return(&inventory.StockLevel{ID: 51, ProductID: 3, BranchID: 5, Qty: 9}, nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.ProductID == 2 && e.Type == inventory.LedgerEntryTypeReturn && e.Qty == 2 && e.BalanceAfter == 7
		})).
		Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.ProductID == 3 && e.Type == inventory.LedgerEntryTypeReturnWriteoff && e.Qty == 0 && e.BalanceAfter == 9
		})).
		Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID,
		Items: []CreateReturnItemRequest{
			{SaleItemID: 1, Qty: 2, Condition: ItemConditionSellable},
			{SaleItemID: 2, Qty: 1, Condition: ItemConditionExpired},
		},
		ReasonCode:   ReturnReasonCodeOther,
		RefundMethod: RefundMethodQR,
	})
	require.NoError(t, err)
}

// A product that has never had a stock row still gets its write-off entry;
// the balance is simply 0, and no stock row is invented for it.
func TestService_CreateReturn_WriteoffForProductWithNoStockRow_UsesZeroBalance(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 1, LineTotal: d("10.00")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, sales.SaleStatusRefunded).Return(nil).Once() // the one line is fully back
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Return) { r.ID = 402 }).Return(nil).Once()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(nil, nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.Type == inventory.LedgerEntryTypeReturnWriteoff && e.Qty == 0 && e.BalanceAfter == 0
		})).
		Return(nil).Once()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, damagedReturn(saleID))
	require.NoError(t, err)
}

// returnAndObserveSaleStatus runs a Return of requested lines against a sale
// with the given lines, where returnedBefore/exchangedIn are what earlier
// returns and exchange "in" lines already took per sale item. It expects
// UpdateSaleStatus(refunded) exactly when wantRefunded - any other status
// write, or a missing one, fails the test (an unexpected mock call).
func returnAndObserveSaleStatus(t *testing.T, saleLines []sales.SaleItem, returnedBefore, exchangedIn map[uint]int,
	request []CreateReturnItemRequest, wantRefunded bool) error {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	for i := range saleLines {
		saleLines[i].SaleID = saleID
		repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, saleLines[i].ID).Return(returnedBefore[saleLines[i].ID], nil).Maybe()
		repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, saleLines[i].ID).Return(exchangedIn[saleLines[i].ID], nil).Maybe()
	}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(saleLines, nil).Once()
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Return) { r.ID = 500 }).Return(nil).Maybe()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Maybe()
	inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, mock.Anything).Return(&inventory.StockLevel{ID: 50, Qty: 10}, nil).Maybe()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Maybe()
	if wantRefunded {
		salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, sales.SaleStatusRefunded).Return(nil).Once()
	}

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID, Items: request, ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
	})
	return err
}

func oneLine(id uint, qty int) sales.SaleItem {
	return sales.SaleItem{ID: id, ProductID: id + 10, Qty: qty, LineTotal: d("10.00")}
}

func sellable(id uint, qty int) CreateReturnItemRequest {
	return CreateReturnItemRequest{SaleItemID: id, Qty: qty, Condition: ItemConditionSellable}
}

// Once every line has come back through returns, the sale is "refunded" so
// history can say so (it used to stay "completed" forever).
func TestService_CreateReturn_EveryLineReturnedInFull_MarksSaleRefunded(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 4)}, nil, nil, []CreateReturnItemRequest{sellable(1, 4)}, true)
	require.NoError(t, err)
}

func TestService_CreateReturn_PartialReturn_LeavesSaleCompleted(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 4)}, nil, nil, []CreateReturnItemRequest{sellable(1, 1)}, false)
	require.NoError(t, err)
}

// A later return that finishes off what an earlier one started completes it.
func TestService_CreateReturn_SecondReturnCompletesTheLine_MarksSaleRefunded(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 4)}, map[uint]int{1: 3}, nil, []CreateReturnItemRequest{sellable(1, 1)}, true)
	require.NoError(t, err)
}

func TestService_CreateReturn_OneLineBackButAnotherUntouched_LeavesSaleCompleted(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 2), oneLine(2, 1)}, nil, nil, []CreateReturnItemRequest{sellable(1, 2)}, false)
	require.NoError(t, err)
}

// The other line need not be in this request - it may have come back in an
// earlier return.
func TestService_CreateReturn_OtherLineAlreadyReturnedEarlier_MarksSaleRefunded(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 2), oneLine(2, 1)}, map[uint]int{2: 1}, nil, []CreateReturnItemRequest{sellable(1, 2)}, true)
	require.NoError(t, err)
}

// "Through returns only": a line whose last unit left via an exchange is
// nothing left to return, but the customer swapped it rather than getting a
// refund, so the sale is not "refunded".
func TestService_CreateReturn_RestOfTheLineWasExchangedIn_LeavesSaleCompleted(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 2)}, nil, map[uint]int{1: 1}, []CreateReturnItemRequest{sellable(1, 1)}, false)
	require.NoError(t, err)
}

// Non-sellable goods still count: "refunded" is about money going back, not
// about restocking.
func TestService_CreateReturn_FullReturnOfDamagedGoods_MarksSaleRefunded(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 1)}, nil, nil,
		[]CreateReturnItemRequest{{SaleItemID: 1, Qty: 1, Condition: ItemConditionDamaged}}, true)
	require.NoError(t, err)
}

// Two request lines for the same sale item are checked together, or each
// would pass alone and jointly return more than was sold.
func TestService_CreateReturn_DuplicateLinesForOneSaleItem_AreCheckedTogether(t *testing.T) {
	err := returnAndObserveSaleStatus(t, []sales.SaleItem{oneLine(1, 2)}, nil, nil,
		[]CreateReturnItemRequest{sellable(1, 2), sellable(1, 2)}, false)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// A fully returned sale is settled money-wise, and its refund may already be
// in a closed shift - voiding it would reverse the sale a second time.
func TestService_VoidSale_RefundedSale_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).
		Return(&sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, ShiftID: 9, Status: sales.SaleStatusRefunded}, nil).Once()
	// Nothing else may be consulted: rejected on status alone.

	_, err := svc.VoidSale(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, saleID,
		VoidSaleRequest{Reason: VoidReasonOther})
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateReturn_NoPermission_ReturnsForbidden(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: false}, CreateReturnRequest{
		SaleID: uuid.New(), Items: []CreateReturnItemRequest{{SaleItemID: 1, Qty: 1, Condition: ItemConditionSellable}},
		ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusForbidden)
}

func TestService_CreateReturn_SaleNotCompleted_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusVoided}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	// ListSaleItemsTx/CreateReturn must never be called.

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID, Items: []CreateReturnItemRequest{{SaleItemID: 1, Qty: 1, Condition: ItemConditionSellable}},
		ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateReturn_UnknownSaleItemID_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}, nil).Once()
	// CreateReturn must never be called - rejected before any write.

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID, Items: []CreateReturnItemRequest{{SaleItemID: 99, Qty: 1, Condition: ItemConditionSellable}},
		ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateReturn_OverReturning_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(3, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	// CreateReturn must never be called - 3 already returned + 2 more > 4 originally sold.

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID, Items: []CreateReturnItemRequest{{SaleItemID: 1, Qty: 2, Condition: ItemConditionSellable}},
		ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_GetReturn_DelegatesToRepositoryWithOrgWideBranches(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return([]identity.Branch{{ID: 5, OrgID: 7}}, nil).Once()
	want := &Return{ID: 200, SaleID: uuid.New()}
	repo.EXPECT().GetReturn(mock.Anything, []uint{5}, uint(200)).Return(want, nil).Once()

	got, err := svc.GetReturn(context.Background(), 7, 200)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// --- CreateExchange ---

func TestService_CreateExchange_HappyPath_CreditsInAndDebitsOut(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	svc.products = productsAt(t, 3, "15.00", "0")

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 1, LineTotal: d("10.00")}}

	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().
		CreateExchange(mock.Anything, mock.MatchedBy(func(e *Exchange) bool {
			// out (15.00) - in (10.00) = 5.00
			return e.SaleID == saleID && e.NetDifference.Equal(d("5.00")) && e.ApprovedBy == 30 &&
				e.Method != nil && *e.Method == ExchangeMethodCash
		})).
		Run(func(_ *gorm.DB, e *Exchange) { e.ID = 300 }).
		Return(nil).Once()
	repo.EXPECT().
		CreateExchangeItems(mock.Anything, mock.MatchedBy(func(items []ExchangeItem) bool {
			return len(items) == 2 && items[0].ExchangeID == 300 && items[1].ExchangeID == 300
		})).
		Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 2}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 3).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.Type == inventory.LedgerEntryTypeExchangeIn && e.Qty == 1 && e.BalanceAfter == 3 &&
				e.ReferenceType == inventory.ReferenceTypeExchange && e.ReferenceID == "300"
		})).
		Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(3), uint(5)).Return(&inventory.StockLevel{ID: 51, ProductID: 3, BranchID: 5, Qty: 5}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(51), 4).Return(nil).Once()
	inv.EXPECT().
		CreateInventoryLedgerEntry(mock.Anything, mock.MatchedBy(func(e *inventory.InventoryLedger) bool {
			return e.Type == inventory.LedgerEntryTypeExchangeOut && e.Qty == -1 && e.BalanceAfter == 4 &&
				e.ReferenceType == inventory.ReferenceTypeExchange && e.ReferenceID == "300"
		})).
		Return(nil).Once()
	audW.EXPECT().
		CreateAuditLog(mock.Anything, mock.MatchedBy(func(e *audit.AuditLog) bool {
			return e.OrgID == 7 && e.ActorID != nil && *e.ActorID == 30 && e.BranchID != nil && *e.BranchID == 5 &&
				e.Entity == "sale" && e.EntityID == saleID.String() && e.Action == "exchanged"
		})).
		Return(nil).Once()

	in := CreateExchangeRequest{
		SaleID: saleID,
		Method: ExchangeMethodCash,
		Items: []CreateExchangeItemRequest{
			{Direction: DirectionIn, SaleItemID: uintPtr(1), Qty: 1},
			{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1},
		},
	}
	got, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, in)
	require.NoError(t, err)
	require.Equal(t, uint(300), got.ID)
}

func TestService_CreateExchange_NoPermission_ReturnsForbidden(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: false}, CreateExchangeRequest{
		SaleID: uuid.New(), Items: []CreateExchangeItemRequest{{Direction: DirectionIn, SaleItemID: uintPtr(1), Qty: 1}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusForbidden)
}

func TestService_CreateExchange_OutItemMissingProduct_ReturnsBadRequest(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{}, nil).Once()
	// CreateExchange must never be called - rejected before any write.

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateExchangeRequest{
		SaleID: saleID, Items: []CreateExchangeItemRequest{{Direction: DirectionOut, Qty: 1}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_CreateExchange_InsufficientStockForOutItem_ReturnsConflict(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	svc.products = productsAt(t, 3, "15.00", "0")

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{}, nil).Once()
	repo.EXPECT().CreateExchange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *Exchange) { e.ID = 301 }).Return(nil).Once()
	repo.EXPECT().CreateExchangeItems(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(3), uint(5)).Return(&inventory.StockLevel{ID: 51, ProductID: 3, BranchID: 5, Qty: 0}, nil).Once()
	// UpdateStockLevelQty/CreateInventoryLedgerEntry must never be called -
	// the movement is rejected before either write happens.

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateExchangeRequest{
		SaleID: saleID,
		Method: ExchangeMethodCash,
		Items:  []CreateExchangeItemRequest{{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func uintPtr(v uint) *uint { return &v }

// --- Shift linkage (Close Shift expected cash) ---

// shiftTestFixture wires the mocks every shift-linkage return test shares:
// one completed sale with one line, nothing yet returned/exchanged, and a
// non-restocked (damaged) return so no stock movement noise gets in the
// way - these tests only care which shift the Return row is stamped with.
// completes says whether the return is expected to get all the way through
// (items insert + audit entry) or fail partway.
func shiftTestFixture(t *testing.T, completes bool) (*Service, *MockRepository, uuid.UUID) {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 4, LineTotal: d("40.00")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	if completes {
		repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
		inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 3}, nil).Once()
		inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Once()
		audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	}
	return svc, repo, saleID
}

func damagedReturn(saleID uuid.UUID) CreateReturnRequest {
	return CreateReturnRequest{
		SaleID:       saleID,
		Items:        []CreateReturnItemRequest{{SaleItemID: 1, Qty: 1, Condition: ItemConditionDamaged}},
		ReasonCode:   ReturnReasonCodeDefective,
		RefundMethod: RefundMethodCash,
	}
}

// The refund leaves the till's drawer, not the original sale's shift - so
// the Return is stamped with the caller's own open shift.
func TestService_CreateReturn_StampsCallersOpenShift(t *testing.T) {
	svc, repo, saleID := shiftTestFixture(t, true)
	repo.EXPECT().CurrentShiftID(mock.Anything, uint(7), uint(70)).Return(uintPtr(9), nil).Once()
	repo.EXPECT().
		CreateReturn(mock.Anything, mock.MatchedBy(func(r *Return) bool { return r.ShiftID != nil && *r.ShiftID == 9 })).
		Run(func(_ *gorm.DB, r *Return) { r.ID = 200 }).
		Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, PosUserID: 70, CanApprove: true}, damagedReturn(saleID))
	require.NoError(t, err)
}

// No till context at all (not a paired POS account) means there's no
// drawer to attribute the refund to - shift_id stays nil, no lookup made.
func TestService_CreateReturn_NoPosUser_LeavesShiftNil(t *testing.T) {
	svc, repo, saleID := shiftTestFixture(t, true)
	repo.EXPECT().
		CreateReturn(mock.Anything, mock.MatchedBy(func(r *Return) bool { return r.ShiftID == nil })).
		Run(func(_ *gorm.DB, r *Return) { r.ID = 201 }).
		Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, damagedReturn(saleID))
	require.NoError(t, err)
}

// A till with no open shift still gets its return recorded (backward
// compatible with clients that never opened one) - it just isn't counted in
// any shift's expected cash.
func TestService_CreateReturn_NoOpenShift_LeavesShiftNil(t *testing.T) {
	svc, repo, saleID := shiftTestFixture(t, true)
	repo.EXPECT().CurrentShiftID(mock.Anything, uint(7), uint(70)).Return(nil, nil).Once()
	repo.EXPECT().
		CreateReturn(mock.Anything, mock.MatchedBy(func(r *Return) bool { return r.ShiftID == nil })).
		Run(func(_ *gorm.DB, r *Return) { r.ID = 202 }).
		Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, PosUserID: 70, CanApprove: true}, damagedReturn(saleID))
	require.NoError(t, err)
}

func TestService_CreateReturn_ShiftLookupFails_PropagatesAsIs(t *testing.T) {
	svc, repo, saleID := shiftTestFixture(t, false)
	dbErr := errors.New("connection refused")
	repo.EXPECT().CurrentShiftID(mock.Anything, uint(7), uint(70)).Return(nil, dbErr).Once()
	// CreateReturn must never be called once the lookup itself fails - no
	// .EXPECT() set up for it means the mock fails the test if it is.

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, PosUserID: 70, CanApprove: true}, damagedReturn(saleID))
	require.ErrorIs(t, err, dbErr)
}

// exchangeShiftFixture wires one completed sale (one line: product 2, qty 1,
// 10.00) with nothing yet returned/exchanged. When completes is true it also
// expects the stock/ledger/audit writes of a "1 in, 1 out (product 3)"
// exchange to succeed.
func exchangeShiftFixture(t *testing.T, completes bool) (*Service, *MockRepository, uuid.UUID) {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	// product 3 is priced at 10.00 and 15.00 by different tests, so the lookup
	// is left open (and unrestricted) here
	products := NewMockProductLookup(t)
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(3)).Return(&catalog.Product{ID: 3, OrgID: 7, Price: d("15.00"), SalePrice: d("15.00")}, nil).Maybe()
	products.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(5), uint(3)).Return(nil, nil).Maybe()
	svc.products = products

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 1, LineTotal: d("10.00")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	if completes {
		repo.EXPECT().CreateExchangeItems(mock.Anything, mock.Anything).Return(nil).Once()
		inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 2}, nil).Once()
		inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 3).Return(nil).Once()
		inv.EXPECT().GetStockLevel(mock.Anything, uint(3), uint(5)).Return(&inventory.StockLevel{ID: 51, ProductID: 3, BranchID: 5, Qty: 5}, nil).Once()
		inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(51), 4).Return(nil).Once()
		inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Twice()
		audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	}
	return svc, repo, saleID
}

// swapForProduct3 is "bring product 2 back, take product 3" - at whatever
// price the product lookup of the test says product 3 sells for (the server
// prices an out line from the product, never from the request).
func swapForProduct3(saleID uuid.UUID, method ExchangeMethod) CreateExchangeRequest {
	return CreateExchangeRequest{
		SaleID: saleID,
		Method: method,
		Items: []CreateExchangeItemRequest{
			{Direction: DirectionIn, SaleItemID: uintPtr(1), Qty: 1},
			{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1},
		},
	}
}

// productsAt is a product lookup in which product `id` is priced at `price`
// (no catalog discount) and carries `taxRate`.
func productsAt(t *testing.T, id uint, price, taxRate string) *MockProductLookup {
	t.Helper()
	products := NewMockProductLookup(t)
	products.EXPECT().GetProduct(mock.Anything, uint(7), id).
		Return(&catalog.Product{ID: id, OrgID: 7, Price: d(price), SalePrice: d(price), TaxPercent: d(taxRate)}, nil).Maybe()
	products.EXPECT().GetBranchPrice(mock.Anything, uint(7), mock.Anything, id).Return(nil, nil).Maybe()
	return products
}

// swapWithInCondition is an even swap ("bring product 2 back in the given
// condition, take product 3 at the same price"), so no method is needed. It
// returns the ExchangeItem rows handed to the repository. restocksIn says
// whether the "in" line is expected to go back on the shelf: when false, no
// stock expectation exists for product 2, so a restock attempt fails the test
// as an unexpected mock call.
func swapWithInCondition(t *testing.T, condition ItemCondition, restocksIn bool) ([]ExchangeItem, []inventory.InventoryLedger) {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	svc.products = productsAt(t, 3, "10.00", "0")

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 1, LineTotal: d("10.00")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().CreateExchange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *Exchange) { e.ID = 320 }).Return(nil).Once()
	var saved []ExchangeItem
	repo.EXPECT().CreateExchangeItems(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, items []ExchangeItem) { saved = items }).Return(nil).Once()
	// Product 2 sits at 2 units. A restocked in-line moves it to 3; a
	// non-restocked one only reads the balance (for the write-off row).
	inv.EXPECT().GetStockLevel(mock.Anything, uint(2), uint(5)).Return(&inventory.StockLevel{ID: 50, ProductID: 2, BranchID: 5, Qty: 2}, nil).Once()
	if restocksIn {
		inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(50), 3).Return(nil).Once()
	}
	inv.EXPECT().GetStockLevel(mock.Anything, uint(3), uint(5)).Return(&inventory.StockLevel{ID: 51, ProductID: 3, BranchID: 5, Qty: 5}, nil).Once()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(51), 4).Return(nil).Once()
	var ledger []inventory.InventoryLedger
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *inventory.InventoryLedger) { ledger = append(ledger, *e) }).Return(nil).Twice()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	req := swapForProduct3(saleID, "")
	req.Items[0].Condition = condition
	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, req)
	require.NoError(t, err)
	return saved, ledger
}

func inLine(t *testing.T, items []ExchangeItem) ExchangeItem {
	t.Helper()
	for _, it := range items {
		if it.Direction == DirectionIn {
			return it
		}
	}
	t.Fatal("no in line saved")
	return ExchangeItem{}
}

// Damaged goods must not go back on the shelf just because they came back
// through an exchange instead of a return.
func TestService_CreateExchange_DamagedInLine_NotRestocked(t *testing.T) {
	saved, _ := swapWithInCondition(t, ItemConditionDamaged, false)
	line := inLine(t, saved)
	require.NotNil(t, line.Condition)
	require.Equal(t, ItemConditionDamaged, *line.Condition)
}

func TestService_CreateExchange_ExpiredAndOtherInLines_NotRestocked(t *testing.T) {
	for _, condition := range []ItemCondition{ItemConditionExpired, ItemConditionOther, ItemConditionOpened, ItemConditionDefective} {
		t.Run(string(condition), func(t *testing.T) {
			saved, _ := swapWithInCondition(t, condition, false)
			line := inLine(t, saved)
			require.NotNil(t, line.Condition)
			require.Equal(t, condition, *line.Condition)
		})
	}
}

func TestService_CreateExchange_SellableInLine_Restocked(t *testing.T) {
	saved, _ := swapWithInCondition(t, ItemConditionSellable, true)
	line := inLine(t, saved)
	require.NotNil(t, line.Condition)
	require.Equal(t, ItemConditionSellable, *line.Condition)
}

// Clients that predate the condition field never send it; those lines were
// always restocked, so an omitted condition means sellable (and is stored as
// such, so the row reads the same as an explicit sellable one).
func TestService_CreateExchange_OmittedCondition_TreatedAsSellableAndRestocked(t *testing.T) {
	saved, _ := swapWithInCondition(t, "", true)
	line := inLine(t, saved)
	require.NotNil(t, line.Condition)
	require.Equal(t, ItemConditionSellable, *line.Condition)
}

// The same visibility gap as a return: a non-sellable in-line moves no stock,
// so it gets a zero-qty write-off row next to the exchange_out one.
func TestService_CreateExchange_DamagedInLine_WritesOffLedgerEntry(t *testing.T) {
	_, ledger := swapWithInCondition(t, ItemConditionDamaged, false)
	require.Len(t, ledger, 2)
	var writeoff, out *inventory.InventoryLedger
	for i := range ledger {
		switch ledger[i].Type {
		case inventory.LedgerEntryTypeReturnWriteoff:
			writeoff = &ledger[i]
		case inventory.LedgerEntryTypeExchangeOut:
			out = &ledger[i]
		}
	}
	require.NotNil(t, writeoff)
	require.Equal(t, uint(2), writeoff.ProductID)
	require.Equal(t, 0, writeoff.Qty)
	require.Equal(t, 2, writeoff.BalanceAfter)
	require.Equal(t, inventory.ReferenceTypeExchange, writeoff.ReferenceType)
	require.Equal(t, "320", writeoff.ReferenceID)
	require.NotNil(t, out)
}

func TestService_CreateExchange_SellableInLine_WritesNoWriteoffEntry(t *testing.T) {
	_, ledger := swapWithInCondition(t, ItemConditionSellable, true)
	require.Len(t, ledger, 2)
	for _, e := range ledger {
		require.NotEqual(t, inventory.LedgerEntryTypeReturnWriteoff, e.Type)
	}
}

// An "out" line is a new item going to the customer; a condition means
// nothing there and is not stored.
func TestService_CreateExchange_OutLine_HasNoCondition(t *testing.T) {
	saved, _ := swapWithInCondition(t, ItemConditionSellable, true)
	for _, it := range saved {
		if it.Direction == DirectionOut {
			require.Nil(t, it.Condition)
		}
	}
}

// The method is what lets Close Shift know whether a difference touched the
// drawer, so a non-zero difference can't be recorded without it.
func TestService_CreateExchange_MissingMethodOnNonZeroDifference_ReturnsBadRequest(t *testing.T) {
	svc, _, saleID := exchangeShiftFixture(t, false)
	// CreateExchange must never be called - rejected before anything is
	// written.

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, swapForProduct3(saleID, ""))
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// An even swap moves no money, so there's nothing to say "cash or QR" about.
func TestService_CreateExchange_ZeroDifferenceWithoutMethod_Accepted(t *testing.T) {
	svc, repo, saleID := exchangeShiftFixture(t, true)
	svc.products = productsAt(t, 3, "10.00", "0") // same price as the product that came back
	repo.EXPECT().
		CreateExchange(mock.Anything, mock.MatchedBy(func(e *Exchange) bool {
			return e.NetDifference.IsZero() && e.Method == nil && e.ShiftID == nil
		})).
		Run(func(_ *gorm.DB, e *Exchange) { e.ID = 310 }).
		Return(nil).Once()

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, swapForProduct3(saleID, ""))
	require.NoError(t, err)
}

func TestService_CreateExchange_StampsMethodAndCallersOpenShift(t *testing.T) {
	svc, repo, saleID := exchangeShiftFixture(t, true)
	repo.EXPECT().CurrentShiftID(mock.Anything, uint(7), uint(70)).Return(uintPtr(9), nil).Once()
	repo.EXPECT().
		CreateExchange(mock.Anything, mock.MatchedBy(func(e *Exchange) bool {
			return e.Method != nil && *e.Method == ExchangeMethodQR && e.ShiftID != nil && *e.ShiftID == 9
		})).
		Run(func(_ *gorm.DB, e *Exchange) { e.ID = 311 }).
		Return(nil).Once()

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, PosUserID: 70, CanApprove: true}, swapForProduct3(saleID, ExchangeMethodQR))
	require.NoError(t, err)
}

func TestService_CreateExchange_NoOpenShift_LeavesShiftNil(t *testing.T) {
	svc, repo, saleID := exchangeShiftFixture(t, true)
	repo.EXPECT().CurrentShiftID(mock.Anything, uint(7), uint(70)).Return(nil, nil).Once()
	repo.EXPECT().
		CreateExchange(mock.Anything, mock.MatchedBy(func(e *Exchange) bool { return e.ShiftID == nil })).
		Run(func(_ *gorm.DB, e *Exchange) { e.ID = 312 }).
		Return(nil).Once()

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, PosUserID: 70, CanApprove: true}, swapForProduct3(saleID, ExchangeMethodCash))
	require.NoError(t, err)
}

// --- Tax inside refunds and exchanges (Tax V1) ---

// returnLine returns qty units of one sale line (sellable, cash) and gives back
// the Return the service stored. Everything around it is set up generously: these
// tests only care about the money.
func returnLine(t *testing.T, line sales.SaleItem, qty int) Return {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)

	saleID := uuid.New()
	line.ID, line.SaleID, line.ProductID = 1, saleID, 2
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).
		Return(&sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{line}, nil).Once()
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, mock.Anything).Return(nil).Maybe()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Maybe()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	var saved Return
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, r *Return) { r.ID = 400; saved = *r }).Return(nil).Once()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, mock.Anything).Return(&inventory.StockLevel{ID: 50, Qty: 10}, nil).Maybe()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID, Items: []CreateReturnItemRequest{sellable(1, qty)}, ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
	})
	require.NoError(t, err)
	return saved
}

// The refund is what the customer paid for the line (tax already inside it);
// TaxRefunded is just the part of that which is tax, at the line's stored rate.
func TestService_CreateReturn_FullReturnOfATaxedLine_RefundsTheLineTotalAndTheTaxInIt(t *testing.T) {
	got := returnLine(t, sales.SaleItem{Qty: 4, LineTotal: d("42000"), TaxPercent: d("5"), Tax: d("2000")}, 4)

	require.True(t, d("42000").Equal(got.RefundTotal))
	require.True(t, d("2000").Equal(got.TaxRefunded), "42,000 x 5 / 105 (got %s)", got.TaxRefunded)
}

func TestService_CreateReturn_PartialReturnOfATaxedLine_RefundsTheShareAndItsTax(t *testing.T) {
	got := returnLine(t, sales.SaleItem{Qty: 3, LineTotal: d("10500"), TaxPercent: d("5"), Tax: d("500")}, 1)

	require.True(t, d("3500").Equal(got.RefundTotal))
	require.True(t, d("166.67").Equal(got.TaxRefunded), "3,500 x 5 / 105 (got %s)", got.TaxRefunded)
}

// A line sold at a discount refunds what was actually paid, so the tax given
// back is the (smaller) tax inside that.
func TestService_CreateReturn_DiscountedTaxedLine_RefundsTheTaxOfWhatWasPaid(t *testing.T) {
	// 2 x 6,500 less 10% = 11,800 paid, 561.90 tax inside it
	got := returnLine(t, sales.SaleItem{Qty: 2, LineTotal: d("11800"), TaxPercent: d("5"), Tax: d("561.90")}, 1)

	require.True(t, d("5900").Equal(got.RefundTotal))
	require.True(t, d("280.95").Equal(got.TaxRefunded))
}

// A line from before tax was a rate has no stored rate: its refund is as it
// always was and no tax is reported back.
func TestService_CreateReturn_LineFromBeforeTaxWasARate_RefundsTheSameAndReportsNoTax(t *testing.T) {
	got := returnLine(t, sales.SaleItem{Qty: 4, LineTotal: d("40.00"), Tax: d("2.00")}, 2)

	require.True(t, d("20.00").Equal(got.RefundTotal))
	require.True(t, got.TaxRefunded.IsZero())
}

func TestService_CreateReturn_TwoLinesWithDifferentRates_SumTheirTaxRefunded(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	saleID := uuid.New()
	lines := []sales.SaleItem{
		{ID: 1, SaleID: saleID, ProductID: 2, Qty: 1, LineTotal: d("10500"), TaxPercent: d("5")},
		{ID: 2, SaleID: saleID, ProductID: 3, Qty: 1, LineTotal: d("1100"), TaxPercent: d("10")},
		{ID: 3, SaleID: saleID, ProductID: 4, Qty: 1, LineTotal: d("700")},
	}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(&sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(lines, nil).Once()
	salesRepo.EXPECT().UpdateSaleStatus(mock.Anything, saleID, mock.Anything).Return(nil).Maybe()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, mock.Anything).Return(0, nil).Maybe()
	var saved Return
	repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, r *Return) { r.ID = 401; saved = *r }).Return(nil).Once()
	repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, mock.Anything).Return(&inventory.StockLevel{ID: 50, Qty: 10}, nil).Maybe()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := svc.CreateReturn(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateReturnRequest{
		SaleID: saleID, ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash,
		Items: []CreateReturnItemRequest{sellable(1, 1), sellable(2, 1), sellable(3, 1)},
	})

	require.NoError(t, err)
	require.True(t, d("12300").Equal(saved.RefundTotal))
	require.True(t, d("600").Equal(saved.TaxRefunded), "500 + 100 + 0 (got %s)", saved.TaxRefunded)
}

// exchangeForProduct3 brings back product 2 (one line: 10,500 paid at 5% tax)
// and takes product 3, which the lookup prices as given. It returns the Exchange
// and the lines the service stored.
func exchangeForProduct3(t *testing.T, product *catalog.Product, req CreateExchangeItemRequest) (*Exchange, []ExchangeItem) {
	t.Helper()
	return exchangeForProduct3At(t, product, nil, req)
}

// exchangeForProduct3At is exchangeForProduct3 where the exchange's branch
// (5, the sale's) carries the given price override of product 3, if any.
func exchangeForProduct3At(t *testing.T, product *catalog.Product, override *catalog.ProductBranchPrice, req CreateExchangeItemRequest) (*Exchange, []ExchangeItem) {
	t.Helper()
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	products := NewMockProductLookup(t)
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(3)).Return(product, nil).Once()
	products.EXPECT().GetBranchPrice(mock.Anything, uint(7), uint(5), uint(3)).Return(override, nil).Once()
	svc.products = products

	saleID := uuid.New()
	items := []sales.SaleItem{{ID: 1, SaleID: saleID, ProductID: 2, Qty: 1, LineTotal: d("10500"), TaxPercent: d("5"), Tax: d("500")}}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(&sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return(items, nil).Once()
	repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
	var saved Exchange
	repo.EXPECT().CreateExchange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *Exchange) { e.ID = 330; saved = *e }).Return(nil).Once()
	var savedItems []ExchangeItem
	repo.EXPECT().CreateExchangeItems(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, items []ExchangeItem) { savedItems = items }).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, mock.Anything, mock.Anything).Return(&inventory.StockLevel{ID: 50, Qty: 10}, nil).Maybe()
	inv.EXPECT().UpdateStockLevelQty(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Maybe()
	audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateExchangeRequest{
		SaleID: saleID, Method: ExchangeMethodCash,
		Items: []CreateExchangeItemRequest{{Direction: DirectionIn, SaleItemID: uintPtr(1), Qty: 1}, req},
	})
	require.NoError(t, err)
	require.Equal(t, uint(330), saved.ID)
	return got, savedItems
}

func outLine(t *testing.T, items []ExchangeItem) ExchangeItem {
	t.Helper()
	for _, it := range items {
		if it.Direction == DirectionOut {
			return it
		}
	}
	t.Fatal("no out line saved")
	return ExchangeItem{}
}

// The new goods are priced from the PRODUCT: its sale price (catalog discount
// applied) with its tax already inside - not from a price the client typed, and
// with no tax added on top. 5,900 at 5% carries 280.95 of tax.
func TestService_CreateExchange_OutLine_IsPricedFromTheProductWithItsIncludedTax(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("6500"), DiscountPercent: d("10"), SalePrice: d("5900"), TaxPercent: d("5")}

	got, items := exchangeForProduct3(t, product, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1})

	out := outLine(t, items)
	require.True(t, d("5900").Equal(out.UnitPrice), "the sale price (got %s)", out.UnitPrice)
	require.True(t, d("5").Equal(out.TaxPercent))
	require.True(t, d("280.95").Equal(out.Tax), "(got %s)", out.Tax)
	require.True(t, d("-4600").Equal(got.NetDifference), "out 5,900 - in 10,500; tax is never added on top (got %s)", got.NetDifference)
	require.True(t, d("280.95").Equal(got.TaxOut))
	require.True(t, d("500").Equal(got.TaxIn), "the in line's stored 5%% of 10,500")
	require.Len(t, got.Items, 2, "the response carries the priced lines")
}

// An "out" price the client sends (an old till still does) is not read.
func TestService_CreateExchange_OutLine_ClientUnitPriceIsIgnored(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("6500"), SalePrice: d("6500"), TaxPercent: d("5")}
	cheap := d("1")

	got, items := exchangeForProduct3(t, product, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1, UnitPrice: &cheap})

	require.True(t, d("6500").Equal(outLine(t, items).UnitPrice))
	require.True(t, d("-4000").Equal(got.NetDifference))
}

func TestService_CreateExchange_OutLineQty_MultipliesThePriceAndTheTax(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("10500"), SalePrice: d("10500"), TaxPercent: d("5")}

	got, items := exchangeForProduct3(t, product, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 3})

	require.True(t, d("1500").Equal(outLine(t, items).Tax), "31,500 x 5 / 105")
	require.True(t, d("1500").Equal(got.TaxOut))
	require.True(t, d("21000").Equal(got.NetDifference), "31,500 - 10,500")
}

func TestService_CreateExchange_ZeroPercentOutLine_HasNoTax(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("10500"), SalePrice: d("10500")}

	got, items := exchangeForProduct3(t, product, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1})

	require.True(t, outLine(t, items).Tax.IsZero())
	require.True(t, got.TaxOut.IsZero())
	require.True(t, d("500").Equal(got.TaxIn))
}

// A product row that was never priced (sale_price 0, no percent) sells at its
// price, same as at the till.
func TestService_CreateExchange_OutLine_ProductWithoutAStoredSalePrice_SellsAtItsPrice(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("10500")}

	_, items := exchangeForProduct3(t, product, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1})

	require.True(t, d("10500").Equal(outLine(t, items).UnitPrice))
}

func TestService_CreateExchange_InLine_RecordsItsStoredRateAndTheTaxInTheCredit(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("10500"), SalePrice: d("10500")}

	_, items := exchangeForProduct3(t, product, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1})

	in := inLine(t, items)
	require.True(t, d("5").Equal(in.TaxPercent))
	require.True(t, d("500").Equal(in.Tax))
}

func TestService_CreateExchange_OutLine_UnknownProduct_PropagatesNotFound(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	salesRepo := NewMockSalesReader(t)
	inv := NewMockInventoryWriter(t)
	audW := NewMockAuditWriter(t)
	svc := newTestService(repo, branches, salesRepo, inv, audW)
	products := NewMockProductLookup(t)
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(99)).Return(nil, common.NotFoundError("product not found")).Once()
	svc.products = products
	saleID := uuid.New()
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(&sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{}, nil).Once()
	// CreateExchange must never be called.

	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateExchangeRequest{
		SaleID: saleID, Method: ExchangeMethodCash,
		Items: []CreateExchangeItemRequest{{Direction: DirectionOut, ProductID: uintPtr(99), Qty: 1}},
	})

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

// An exchange happens at the sale's branch, so the new goods are priced there:
// that branch's override of price and percent, not the catalog's. 7,000 at the
// catalog's inherited 10% is 6,300, and the tax inside it follows.
func TestService_CreateExchange_OutLine_IsPricedAtTheBranchOverride(t *testing.T) {
	product := &catalog.Product{ID: 3, OrgID: 7, Price: d("6500"), DiscountPercent: d("10"), SalePrice: d("5900"), TaxPercent: d("5")}
	price := d("7000")
	override := &catalog.ProductBranchPrice{OrgID: 7, ProductID: 3, BranchID: 5, Price: &price, SalePrice: d("6300")}

	got, items := exchangeForProduct3At(t, product, override, CreateExchangeItemRequest{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1})

	out := outLine(t, items)
	require.True(t, d("6300").Equal(out.UnitPrice), "the branch's sale price, not the catalog's 5,900 (got %s)", out.UnitPrice)
	require.True(t, d("300.00").Equal(out.Tax), "tax inside 6,300 at 5%% (got %s)", out.Tax)
	require.True(t, d("-4200").Equal(got.NetDifference), "6,300 out less the 10,500 paid for the line coming back")
}
