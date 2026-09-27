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
	return NewService(repo, branches, salesRepo, inv, auditWriter, fakeTransactioner{})
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
			return v.SaleID == saleID && v.Qty == 3 && v.Reason == VoidReasonStaffError && v.Explanation == "rang up wrong item" && v.ApprovedBy == 30
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
	// GetStockLevel/UpdateStockLevelQty/CreateInventoryLedgerEntry must
	// never be called - a damaged item isn't restocked.
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
			return e.SaleID == saleID && e.NetDifference.Equal(d("5.00")) && e.ApprovedBy == 30
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

	price := d("15.00")
	in := CreateExchangeRequest{
		SaleID: saleID,
		Items: []CreateExchangeItemRequest{
			{Direction: DirectionIn, SaleItemID: uintPtr(1), Qty: 1},
			{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1, UnitPrice: &price},
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

func TestService_CreateExchange_OutItemMissingProductOrPrice_ReturnsBadRequest(t *testing.T) {
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

	saleID := uuid.New()
	sale := &sales.Sale{ID: saleID, OrgID: 7, BranchID: 5, Status: sales.SaleStatusCompleted}
	salesRepo.EXPECT().GetSaleWithLock(mock.Anything, uint(7), saleID).Return(sale, nil).Once()
	salesRepo.EXPECT().ListSaleItemsTx(mock.Anything, saleID).Return([]sales.SaleItem{}, nil).Once()
	repo.EXPECT().CreateExchange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *Exchange) { e.ID = 301 }).Return(nil).Once()
	repo.EXPECT().CreateExchangeItems(mock.Anything, mock.Anything).Return(nil).Once()
	inv.EXPECT().GetStockLevel(mock.Anything, uint(3), uint(5)).Return(&inventory.StockLevel{ID: 51, ProductID: 3, BranchID: 5, Qty: 0}, nil).Once()
	// UpdateStockLevelQty/CreateInventoryLedgerEntry must never be called -
	// the movement is rejected before either write happens.

	price := d("15.00")
	_, err := svc.CreateExchange(context.Background(), 7, Actor{StaffID: 30, CanApprove: true}, CreateExchangeRequest{
		SaleID: saleID,
		Items:  []CreateExchangeItemRequest{{Direction: DirectionOut, ProductID: uintPtr(3), Qty: 1, UnitPrice: &price}},
	})
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusConflict)
}

func uintPtr(v uint) *uint { return &v }
