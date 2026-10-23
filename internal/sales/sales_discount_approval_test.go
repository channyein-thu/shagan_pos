package sales

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

type approvalFixture struct {
	svc      *Service
	repo     *MockRepository
	products *MockProductLookup
	staff    *MockStaffDirectory
	audW     *MockAuditWriter
}

func newApprovalFixture(t *testing.T) *approvalFixture {
	t.Helper()
	repo := NewMockRepository(t)
	products := NewMockProductLookup(t)
	staff := NewMockStaffDirectory(t)
	audW := NewMockAuditWriter(t)
	svc := NewService(repo, NewMockInventoryWriter(t), products, audW, nil, nil, staff, fakeTransactioner{})
	return &approvalFixture{svc: svc, repo: repo, products: products, staff: staff, audW: audW}
}

// A cashier (staff 14) on the till of POS account 70 who may ask for discounts.
func cashierSeller() DiscountSeller {
	return DiscountSeller{StaffID: 14, PosUserID: 70, Permissions: []string{"access_pos_portal", "apply_manual_discount"}}
}

// A manager-level seller who may approve discounts, including their own.
func selfApprovingSeller() DiscountSeller {
	return DiscountSeller{StaffID: 17, PosUserID: 70, Permissions: []string{"apply_manual_discount", "approve_manual_discount", "approve_own_discount"}}
}

func tenPercentOffProduct1(approver uint, pin string) CreateDiscountApprovalRequest {
	return CreateDiscountApprovalRequest{ProductID: 1, Percent: d("10"), Qty: 1, Reason: "damaged box", ApproverStaffID: approver, Pin: pin}
}

// openShiftAndProduct: the till has shift 9 open at branch 3, and product 1
// (6,500) exists in the org.
func (fx *approvalFixture) openShiftAndProduct() {
	fx.repo.EXPECT().FindOpenShift(mock.Anything, uint(7), uint(70)).Return(&OpenShift{ID: 9, BranchID: 3}, nil).Maybe()
	fx.products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(catalogProduct(1, "6500", "0"), nil).Maybe()
}

// ---- approving with another staff member's PIN ----

func TestService_CreateDiscountApproval_CashierWithAManagersPin_IsCreatedAndReturnsTheBackendPrices(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().VerifyApprover(mock.Anything, uint(7), uint(3), uint(17), "222222").
		Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, []string{"approve_manual_discount"}, nil).Once()
	var saved *DiscountApproval
	fx.repo.EXPECT().CreateDiscountApproval(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, a *DiscountApproval) { saved = a }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	got, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(17, "222222"))

	require.NoError(t, err)
	// the prices the till must show: exactly what the backend will charge
	require.True(t, d("6500").Equal(got.UnitPrice))
	require.True(t, d("5900").Equal(got.UnitSalePrice), "6,500 less 10%% is 5,850, rounded up")
	require.Equal(t, uint(1), got.ProductID)
	require.True(t, d("10").Equal(got.Percent))
	require.Equal(t, 1, got.MaxQty)
	require.Equal(t, ApproverRef{ID: 17, Name: "Ko Zaw"}, got.ApprovedBy)
	require.WithinDuration(t, time.Now().Add(DiscountApprovalMaxLifetime), got.ExpiresAt, 5*time.Second)

	require.NotNil(t, saved)
	require.Equal(t, got.ID, saved.ID)
	require.Equal(t, DiscountApprovalApproved, saved.Status)
	require.Equal(t, uint(14), saved.RequestedBy)
	require.Equal(t, uint(17), saved.ApprovedBy)
	require.Equal(t, uint(9), saved.ShiftID, "the till's open shift")
	require.Equal(t, uint(3), saved.BranchID)
	require.Equal(t, uint(7), saved.OrgID)
	require.Equal(t, "damaged box", saved.Reason)
	require.Nil(t, saved.SaleID)

	require.NotNil(t, entry)
	require.Equal(t, "sale", entry.Entity)
	require.Equal(t, "discount_approved", entry.Action)
	require.Equal(t, got.ID.String(), entry.EntityID)
	require.Equal(t, uint(14), *entry.ActorID)
	require.Contains(t, string(entry.After), `"approver":17`)
	require.Contains(t, string(entry.After), `"reason":"damaged box"`)
}

// A wrong PIN is the identity layer's 401 (it also counts toward lockout) and
// nothing is created.
func TestService_CreateDiscountApproval_WrongPin_IsUnauthorizedAndCreatesNothing(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().VerifyApprover(mock.Anything, uint(7), uint(3), uint(17), "000000").
		Return(nil, nil, common.UnauthorizedError("invalid staff or pin")).Once()
	// CreateDiscountApproval must never be called

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(17, "000000"))

	requireRestErrorStatus(t, err, http.StatusUnauthorized)
}

func TestService_CreateDiscountApproval_LockedOutApprover_IsTooManyRequests(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().VerifyApprover(mock.Anything, uint(7), uint(3), uint(17), "222222").
		Return(nil, nil, common.TooManyRequestsError("too many failed PIN attempts")).Once()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(17, "222222"))

	requireRestErrorStatus(t, err, http.StatusTooManyRequests)
}

func TestService_CreateDiscountApproval_ApproverWithoutApproveManualDiscount_IsForbidden(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().VerifyApprover(mock.Anything, uint(7), uint(3), uint(18), "333333").
		Return(&identity.Staff{ID: 18, Name: "Another Cashier"}, []string{"access_pos_portal", "apply_manual_discount"}, nil).Once()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(18, "333333"))

	requireRestErrorStatus(t, err, http.StatusForbidden)
	require.Contains(t, err.Error(), "can't approve discounts")
}

// Typing your OWN pin counts as self-approval: it needs approve_own_discount too.
func TestService_CreateDiscountApproval_SellerEnteringTheirOwnPinWithoutApproveOwn_IsForbidden(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	seller := cashierSeller()
	seller.StaffID = 17
	fx.staff.EXPECT().VerifyApprover(mock.Anything, uint(7), uint(3), uint(17), "222222").
		Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, []string{"approve_manual_discount"}, nil).Once()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, seller, tenPercentOffProduct1(17, "222222"))

	requireRestErrorStatus(t, err, http.StatusForbidden)
	require.Contains(t, err.Error(), "someone else")
}

func TestService_CreateDiscountApproval_SellerEnteringTheirOwnPinWithBothPermissions_IsCreated(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().VerifyApprover(mock.Anything, uint(7), uint(3), uint(17), "222222").
		Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, []string{"approve_manual_discount", "approve_own_discount"}, nil).Once()
	fx.repo.EXPECT().CreateDiscountApproval(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()

	got, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, selfApprovingSeller(), tenPercentOffProduct1(17, "222222"))

	require.NoError(t, err)
	require.Equal(t, uint(17), got.ApprovedBy.ID)
}

// ---- self-approval (no PIN pad) ----

func TestService_CreateDiscountApproval_SelfApproval_WithBothPermissions_IsCreatedWithoutAPinCheck(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17, Name: "Ko Zaw"}, nil).Once()
	var saved *DiscountApproval
	fx.repo.EXPECT().CreateDiscountApproval(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, a *DiscountApproval) { saved = a }).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	// VerifyApprover must never be called: there is no PIN pad for this.

	got, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, selfApprovingSeller(), tenPercentOffProduct1(0, ""))

	require.NoError(t, err)
	require.Equal(t, ApproverRef{ID: 17, Name: "Ko Zaw"}, got.ApprovedBy)
	require.Equal(t, uint(17), saved.RequestedBy)
	require.Equal(t, uint(17), saved.ApprovedBy)
}

func TestService_CreateDiscountApproval_SelfApproval_WithoutApproveOwnDiscount_IsForbidden(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	seller := DiscountSeller{StaffID: 17, PosUserID: 70, Permissions: []string{"apply_manual_discount", "approve_manual_discount"}}

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, seller, tenPercentOffProduct1(0, ""))

	requireRestErrorStatus(t, err, http.StatusForbidden)
	require.Contains(t, err.Error(), "needs approval from someone else")
}

func TestService_CreateDiscountApproval_SelfApproval_ByACashier_IsForbidden(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(0, ""))

	requireRestErrorStatus(t, err, http.StatusForbidden)
	require.Contains(t, err.Error(), "needs approval from someone else")
}

func TestService_CreateDiscountApproval_SelfApproval_WithOnlyApproveOwn_IsForbidden(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	seller := DiscountSeller{StaffID: 17, PosUserID: 70, Permissions: []string{"apply_manual_discount", "approve_own_discount"}}

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, seller, tenPercentOffProduct1(0, ""))

	requireRestErrorStatus(t, err, http.StatusForbidden)
}

// ---- who may ask, and when ----

func TestService_CreateDiscountApproval_SellerWithoutApplyManualDiscount_IsForbiddenBeforeAnythingElse(t *testing.T) {
	fx := newApprovalFixture(t) // no expectation of any kind: nothing may be consulted
	seller := DiscountSeller{StaffID: 14, PosUserID: 70, Permissions: []string{"access_pos_portal"}}

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, seller, tenPercentOffProduct1(17, "222222"))

	requireRestErrorStatus(t, err, http.StatusForbidden)
	require.Contains(t, err.Error(), "Your role can't ask for discounts.")
}

func TestService_CreateDiscountApproval_NoOpenShiftOnTheTill_IsConflictAndNoPinIsChecked(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.repo.EXPECT().FindOpenShift(mock.Anything, uint(7), uint(70)).Return(nil, nil).Once()
	// VerifyApprover must never be called: a PIN attempt counts toward lockout.

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(17, "222222"))

	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateDiscountApproval_OpenShiftAtAnotherBranch_IsConflict(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.repo.EXPECT().FindOpenShift(mock.Anything, uint(7), uint(70)).Return(&OpenShift{ID: 9, BranchID: 99}, nil).Once()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(17, "222222"))

	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CreateDiscountApproval_ProductNotInTheOrg_IsNotFoundAndNoPinIsChecked(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.repo.EXPECT().FindOpenShift(mock.Anything, uint(7), uint(70)).Return(&OpenShift{ID: 9, BranchID: 3}, nil).Once()
	fx.products.EXPECT().GetProduct(mock.Anything, uint(7), uint(1)).Return(nil, common.NotFoundError("product not found")).Once()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), tenPercentOffProduct1(17, "222222"))

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateDiscountApproval_RejectsABadRequest_BeforeTouchingAnything(t *testing.T) {
	cases := map[string]func(in *CreateDiscountApprovalRequest){
		"percent zero":               func(in *CreateDiscountApprovalRequest) { in.Percent = d("0") },
		"percent negative":           func(in *CreateDiscountApprovalRequest) { in.Percent = d("-5") },
		"percent over 100":           func(in *CreateDiscountApprovalRequest) { in.Percent = d("100.01") },
		"zero quantity":              func(in *CreateDiscountApprovalRequest) { in.Qty = 0 },
		"reason over 200 characters": func(in *CreateDiscountApprovalRequest) { in.Reason = string(make([]rune, 201)) + "x" },
		"approver without a pin":     func(in *CreateDiscountApprovalRequest) { in.Pin = "" },
		"pin without an approver":    func(in *CreateDiscountApprovalRequest) { in.ApproverStaffID = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newApprovalFixture(t) // nothing may be consulted
			in := tenPercentOffProduct1(17, "222222")
			mutate(&in)
			if name == "reason over 200 characters" {
				in.Reason = ""
				for i := 0; i < 201; i++ {
					in.Reason += "x"
				}
			}

			_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, cashierSeller(), in)

			requireRestErrorStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestService_CreateDiscountApproval_HundredPercentIsAllowed(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.repo.EXPECT().CreateDiscountApproval(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	fx.staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17}, nil).Once()
	in := tenPercentOffProduct1(0, "")
	in.Percent = d("100")

	got, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, selfApprovingSeller(), in)

	require.NoError(t, err)
	require.True(t, got.UnitSalePrice.IsZero())
}

func TestService_CreateDiscountApproval_AuditFailure_FailsTheWholeCreate(t *testing.T) {
	fx := newApprovalFixture(t)
	fx.openShiftAndProduct()
	fx.staff.EXPECT().GetStaff(mock.Anything, uint(7), uint(17)).Return(&identity.Staff{ID: 17}, nil).Once()
	fx.repo.EXPECT().CreateDiscountApproval(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(errors.New("audit down")).Once()

	_, err := fx.svc.CreateDiscountApproval(context.Background(), 7, 3, selfApprovingSeller(), tenPercentOffProduct1(0, ""))

	require.Error(t, err)
}

// ---- cancelling ----

func existingApproval(status DiscountApprovalStatus) *DiscountApproval {
	a := validApproval()
	a.Status = status
	return &a
}

func TestService_CancelDiscountApproval_RemovesAnApprovedDiscount_Audited(t *testing.T) {
	fx := newApprovalFixture(t)
	a := existingApproval(DiscountApprovalApproved)
	fx.repo.EXPECT().GetDiscountApproval(mock.Anything, uint(7), a.ID).Return(a, nil).Once()
	fx.repo.EXPECT().CancelDiscountApproval(mock.Anything, a.ID).Return(true, nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()

	err := fx.svc.CancelDiscountApproval(context.Background(), 7, 14, a.ID)

	require.NoError(t, err)
	require.Equal(t, "discount_cancelled", entry.Action)
	require.Equal(t, "sale", entry.Entity)
	require.Equal(t, a.ID.String(), entry.EntityID)
}

func TestService_CancelDiscountApproval_AlreadyUsed_IsConflict(t *testing.T) {
	fx := newApprovalFixture(t)
	a := existingApproval(DiscountApprovalUsed)
	fx.repo.EXPECT().GetDiscountApproval(mock.Anything, uint(7), a.ID).Return(a, nil).Once()

	err := fx.svc.CancelDiscountApproval(context.Background(), 7, 14, a.ID)

	requireRestErrorStatus(t, err, http.StatusConflict)
}

// Spent by a sale in the instant between the read and the conditional update.
func TestService_CancelDiscountApproval_SpentInTheMeantime_IsConflict(t *testing.T) {
	fx := newApprovalFixture(t)
	a := existingApproval(DiscountApprovalApproved)
	fx.repo.EXPECT().GetDiscountApproval(mock.Anything, uint(7), a.ID).Return(a, nil).Once()
	fx.repo.EXPECT().CancelDiscountApproval(mock.Anything, a.ID).Return(false, nil).Once()

	err := fx.svc.CancelDiscountApproval(context.Background(), 7, 14, a.ID)

	requireRestErrorStatus(t, err, http.StatusConflict)
}

func TestService_CancelDiscountApproval_AlreadyCancelled_IsANoOp(t *testing.T) {
	fx := newApprovalFixture(t)
	a := existingApproval(DiscountApprovalCancelled)
	fx.repo.EXPECT().GetDiscountApproval(mock.Anything, uint(7), a.ID).Return(a, nil).Once()
	// no write, no audit entry

	require.NoError(t, fx.svc.CancelDiscountApproval(context.Background(), 7, 14, a.ID))
}

// Someone else's approval is "not found", never "forbidden": a till has no
// business learning which approval ids exist.
func TestService_CancelDiscountApproval_AnotherSellersApproval_IsNotFound(t *testing.T) {
	fx := newApprovalFixture(t)
	a := existingApproval(DiscountApprovalApproved)
	fx.repo.EXPECT().GetDiscountApproval(mock.Anything, uint(7), a.ID).Return(a, nil).Once()

	err := fx.svc.CancelDiscountApproval(context.Background(), 7, 99, a.ID)

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CancelDiscountApproval_UnknownOrForeignApproval_IsNotFound(t *testing.T) {
	fx := newApprovalFixture(t)
	id := uuid.New()
	fx.repo.EXPECT().GetDiscountApproval(mock.Anything, uint(7), id).Return(nil, common.NotFoundError("discount approval not found")).Once()

	err := fx.svc.CancelDiscountApproval(context.Background(), 7, 14, id)

	requireRestErrorStatus(t, err, http.StatusNotFound)
}
