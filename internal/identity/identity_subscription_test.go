package identity

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/subscription"
)

type subsFixture struct {
	svc  *Service
	repo *MockRepository
	subs *MockSubscriptions
	audW *MockAuditWriter
	// calls records the order of the limit check and the write, to prove the
	// check runs first and inside the same transaction as the write
	calls *[]string
}

func newSubsFixture(t *testing.T) *subsFixture {
	t.Helper()
	repo, subs, audW := NewMockRepository(t), NewMockSubscriptions(t), NewMockAuditWriter(t)
	return &subsFixture{svc: newServiceWithSubscriptions(repo, audW, subs), repo: repo, subs: subs, audW: audW, calls: &[]string{}}
}

func (fx *subsFixture) record(name string) { *fx.calls = append(*fx.calls, name) }

func (fx *subsFixture) limitOK(orgID uint, kind subscription.LimitKind) {
	fx.subs.EXPECT().EnforceLimit(mock.Anything, orgID, kind, mock.Anything).
		Run(func(*gorm.DB, uint, subscription.LimitKind, time.Time) { fx.record("check") }).Return(nil).Once()
}

func (fx *subsFixture) limitReached(orgID uint, kind subscription.LimitKind, max, current int) {
	fx.subs.EXPECT().EnforceLimit(mock.Anything, orgID, kind, mock.Anything).
		Return(subscription.LimitReachedError(kind, max, current)).Once()
}

func requireLimitReached(t *testing.T, err error, kind string, max, current int) {
	t.Helper()
	requireRestErrorStatus(t, err, http.StatusForbidden)
	var restErr common.RestError
	require.ErrorAs(t, err, &restErr)
	require.Equal(t, subscription.CodePlanLimitReached, restErr.Code)
	require.Equal(t, kind, restErr.Details["limit"])
	require.Equal(t, max, restErr.Details["max"])
	require.Equal(t, current, restErr.Details["current"])
}

// ---- creating a branch ----

func TestService_CreateBranch_Active_ChecksTheLimitBeforeInsertingInTheSameTransaction(t *testing.T) {
	fx := newSubsFixture(t)
	in := CreateBranchRequest{Name: "Second", Status: BranchStatusActive, Address: "x", Phone: "1"}
	fx.limitOK(7, subscription.LimitBranches)
	fx.repo.EXPECT().CreateBranch(mock.Anything, uint(7), in).
		Run(func(*gorm.DB, uint, CreateBranchRequest) { fx.record("insert") }).Return(&Branch{ID: 3}, nil).Once()

	got, err := fx.svc.CreateBranch(context.Background(), 7, in)

	require.NoError(t, err)
	require.Equal(t, uint(3), got.ID)
	require.Equal(t, []string{"check", "insert"}, *fx.calls)
}

func TestService_CreateBranch_AtTheLimit_IsRefusedAndNothingIsInserted(t *testing.T) {
	fx := newSubsFixture(t)
	fx.limitReached(7, subscription.LimitBranches, 1, 1)
	// repo.CreateBranch must not be called

	_, err := fx.svc.CreateBranch(context.Background(), 7, CreateBranchRequest{Name: "Second", Status: BranchStatusActive, Address: "x", Phone: "1"})

	requireLimitReached(t, err, "branches", 1, 1)
}

// An inactive branch isn't counted, so creating one is never blocked.
func TestService_CreateBranch_Inactive_IsNeverCounted(t *testing.T) {
	fx := newSubsFixture(t)
	in := CreateBranchRequest{Name: "Closed", Status: BranchStatusInactive, Address: "x", Phone: "1"}
	fx.repo.EXPECT().CreateBranch(mock.Anything, uint(7), in).Return(&Branch{ID: 4}, nil).Once()
	// no EnforceLimit expectation

	_, err := fx.svc.CreateBranch(context.Background(), 7, in)

	require.NoError(t, err)
}

// ---- creating a device ----

func TestService_CreateDevice_Active_ChecksTheLimitBeforeInsertingInTheSameTransaction(t *testing.T) {
	fx := newSubsFixture(t)
	in := CreateDeviceRequest{BranchID: 1, Name: "Till 3", Status: DeviceStatusActive}
	fx.repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(1)).Return(&Branch{ID: 1, OrgID: 7}, nil).Once()
	fx.limitOK(7, subscription.LimitDevices)
	fx.repo.EXPECT().CreateDevice(mock.Anything, uint(7), in).
		Run(func(*gorm.DB, uint, CreateDeviceRequest) { fx.record("insert") }).Return(&Device{ID: 9}, nil).Once()

	_, err := fx.svc.CreateDevice(context.Background(), 7, in)

	require.NoError(t, err)
	require.Equal(t, []string{"check", "insert"}, *fx.calls)
}

func TestService_CreateDevice_AtTheLimit_IsRefused(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(1)).Return(&Branch{ID: 1, OrgID: 7}, nil).Once()
	fx.limitReached(7, subscription.LimitDevices, 2, 2)

	_, err := fx.svc.CreateDevice(context.Background(), 7, CreateDeviceRequest{BranchID: 1, Name: "Till 3", Status: DeviceStatusActive})

	requireLimitReached(t, err, "devices", 2, 2)
}

// Another org's branch is a 404 whatever the plan - the limit isn't even looked at.
func TestService_CreateDevice_ForeignBranch_IsNotFoundBeforeTheLimit(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(99)).Return(nil, common.NotFoundError("branch not found")).Once()

	_, err := fx.svc.CreateDevice(context.Background(), 7, CreateDeviceRequest{BranchID: 99, Name: "Sneaky", Status: DeviceStatusActive})

	requireRestErrorStatus(t, err, http.StatusNotFound)
}

func TestService_CreateDevice_Inactive_IsNeverCounted(t *testing.T) {
	fx := newSubsFixture(t)
	in := CreateDeviceRequest{BranchID: 1, Name: "Spare", Status: DeviceStatusInactive}
	fx.repo.EXPECT().CreateDevice(mock.Anything, uint(7), in).Return(&Device{ID: 5}, nil).Once()

	_, err := fx.svc.CreateDevice(context.Background(), 7, in)

	require.NoError(t, err)
}

// ---- the owner reactivating through PATCH ----

func TestService_UpdateBranch_ReactivatingAnInactiveBranch_ChecksTheLimit(t *testing.T) {
	fx := newSubsFixture(t)
	active := BranchStatusActive
	in := UpdateBranchRequest{Status: &active}
	fx.repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(&Branch{ID: 3, OrgID: 7, Status: BranchStatusInactive}, nil).Once()
	fx.limitOK(7, subscription.LimitBranches)
	fx.repo.EXPECT().UpdateBranch(mock.Anything, uint(7), uint(3), in).
		Run(func(*gorm.DB, uint, uint, UpdateBranchRequest) { fx.record("update") }).Return(&Branch{ID: 3, Status: BranchStatusActive}, nil).Once()

	_, err := fx.svc.UpdateBranch(context.Background(), 7, 3, in)

	require.NoError(t, err)
	require.Equal(t, []string{"check", "update"}, *fx.calls)
}

func TestService_UpdateBranch_ReactivatingAtTheLimit_IsRefusedAndNotUpdated(t *testing.T) {
	fx := newSubsFixture(t)
	active := BranchStatusActive
	fx.repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(&Branch{ID: 3, OrgID: 7, Status: BranchStatusInactive}, nil).Once()
	fx.limitReached(7, subscription.LimitBranches, 1, 1)

	_, err := fx.svc.UpdateBranch(context.Background(), 7, 3, UpdateBranchRequest{Status: &active})

	requireLimitReached(t, err, "branches", 1, 1)
}

// An edit that leaves the branch active (or doesn't touch its status) adds nothing.
func TestService_UpdateBranch_NotAReactivation_IsNeverCounted(t *testing.T) {
	active, inactive, name := BranchStatusActive, BranchStatusInactive, "Renamed"
	for label, in := range map[string]UpdateBranchRequest{
		"renamed":            {Name: &name},
		"already active":     {Status: &active},
		"being switched off": {Status: &inactive},
	} {
		t.Run(label, func(t *testing.T) {
			fx := newSubsFixture(t)
			if label == "already active" {
				fx.repo.EXPECT().GetBranch(mock.Anything, uint(7), uint(3)).Return(&Branch{ID: 3, OrgID: 7, Status: BranchStatusActive}, nil).Once()
			}
			fx.repo.EXPECT().UpdateBranch(mock.Anything, uint(7), uint(3), in).Return(&Branch{ID: 3}, nil).Once()

			_, err := fx.svc.UpdateBranch(context.Background(), 7, 3, in)

			require.NoError(t, err)
		})
	}
}

func TestService_UpdateDevice_ReactivatingARevokedDevice_ChecksTheLimit(t *testing.T) {
	fx := newSubsFixture(t)
	active := DeviceStatusActive
	in := UpdateDeviceRequest{Status: &active}
	fx.repo.EXPECT().GetDevice(mock.Anything, uint(7), uint(5)).Return(&Device{ID: 5, Status: DeviceStatusRevoked}, nil).Once()
	fx.limitReached(7, subscription.LimitDevices, 2, 2)

	_, err := fx.svc.UpdateDevice(context.Background(), 7, 5, in)

	requireLimitReached(t, err, "devices", 2, 2)
}

func TestService_UpdateDevice_AlreadyActive_IsNeverCounted(t *testing.T) {
	fx := newSubsFixture(t)
	active := DeviceStatusActive
	in := UpdateDeviceRequest{Status: &active}
	fx.repo.EXPECT().GetDevice(mock.Anything, uint(7), uint(5)).Return(&Device{ID: 5, Status: DeviceStatusActive}, nil).Once()
	fx.repo.EXPECT().UpdateDevice(mock.Anything, uint(7), uint(5), in).Return(&Device{ID: 5}, nil).Once()

	_, err := fx.svc.UpdateDevice(context.Background(), 7, 5, in)

	require.NoError(t, err)
}

// ---- /internal reactivation and creates share the same rule ----

func TestService_UpdateBranchStatus_ReactivatingIsCheckedAgainstTheBranchsOwnOrg(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().GetBranchAnyOrg(mock.Anything, uint(3)).Return(&Branch{ID: 3, OrgID: 41, Status: BranchStatusInactive}, nil).Once()
	fx.limitReached(41, subscription.LimitBranches, 1, 1)

	err := fx.svc.UpdateBranchStatus(context.Background(), 3, BranchStatusActive)

	requireLimitReached(t, err, "branches", 1, 1)
}

func TestService_UpdateBranchStatus_ReactivatingWithinTheLimit_CheckedThenWritten(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().GetBranchAnyOrg(mock.Anything, uint(3)).Return(&Branch{ID: 3, OrgID: 41, Status: BranchStatusInactive}, nil).Once()
	fx.limitOK(41, subscription.LimitBranches)
	fx.repo.EXPECT().UpdateBranchStatusInternal(mock.Anything, uint(3), BranchStatusActive).
		Run(func(*gorm.DB, uint, BranchStatus) { fx.record("write") }).Return(nil).Once()

	require.NoError(t, fx.svc.UpdateBranchStatus(context.Background(), 3, BranchStatusActive))
	require.Equal(t, []string{"check", "write"}, *fx.calls)
}

// Switching something OFF - or "reactivating" what is already on - is never blocked.
func TestService_UpdateBranchAndDeviceStatus_DeactivatingOrAlreadyActive_IsNeverCounted(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().UpdateBranchStatusInternal(mock.Anything, uint(3), BranchStatusInactive).Return(nil).Once()
	require.NoError(t, fx.svc.UpdateBranchStatus(context.Background(), 3, BranchStatusInactive))
	fx.repo.EXPECT().UpdateDeviceStatusInternal(mock.Anything, uint(5), DeviceStatusRevoked).Return(nil).Once()
	require.NoError(t, fx.svc.UpdateDeviceStatus(context.Background(), 5, DeviceStatusRevoked))

	fx.repo.EXPECT().GetBranchAnyOrg(mock.Anything, uint(4)).Return(&Branch{ID: 4, OrgID: 41, Status: BranchStatusActive}, nil).Once()
	fx.repo.EXPECT().UpdateBranchStatusInternal(mock.Anything, uint(4), BranchStatusActive).Return(nil).Once()
	require.NoError(t, fx.svc.UpdateBranchStatus(context.Background(), 4, BranchStatusActive))
}

func TestService_UpdateDeviceStatus_Reactivating_IsCheckedAgainstTheDevicesOrg(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().GetDeviceAnyOrg(mock.Anything, uint(5)).Return(&Device{ID: 5, Status: DeviceStatusInactive}, uint(41), nil).Once()
	fx.limitReached(41, subscription.LimitDevices, 2, 2)

	err := fx.svc.UpdateDeviceStatus(context.Background(), 5, DeviceStatusActive)

	requireLimitReached(t, err, "devices", 2, 2)
}

// ---- a new organization starts on the Pro trial ----

func TestService_CreateAccount_StartsAFourteenDayProTrial_WithHistoryAndAudit(t *testing.T) {
	fx := newSubsFixture(t)
	var org *Organization
	fx.repo.EXPECT().CreateOrganization(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, o *Organization) { o.ID = 41; org = o }).Return(nil).Once()
	var change *subscription.OrgPlanChange
	fx.subs.EXPECT().RecordChange(mock.Anything, mock.Anything).
		Run(func(_ *gorm.DB, c *subscription.OrgPlanChange) { change = c }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()
	fx.repo.EXPECT().ProvisionDefaultRoles(mock.Anything, uint(41)).Return(nil).Once()
	fx.repo.EXPECT().CreateUser(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, u *User) { u.ID = 1 }).Return(nil).Twice()
	before := time.Now()

	_, err := fx.svc.CreateAccount(context.Background(), CreateAccountInput{
		OrganizationName: "New Org", OwnerEmail: "o@new.test", OwnerPassword: "correct horse battery",
		ServiceCenterEmail: "sc@new.test", ServiceCenterPassword: "another strong password", ChangedBy: "admin@shagan.test",
	})

	require.NoError(t, err)
	require.Equal(t, "pro", org.Plan)
	require.True(t, org.PlanTrial)
	require.NotNil(t, org.PlanExpiresAt)
	require.WithinDuration(t, before.Add(14*24*time.Hour), *org.PlanExpiresAt, time.Minute)
	require.Zero(t, org.ExtraBranches)

	require.NotNil(t, change, "the trial is the first history row")
	require.Equal(t, uint(41), change.OrgID)
	require.Equal(t, "", change.FromPlan)
	require.Equal(t, "pro", change.ToPlan)
	require.True(t, change.Trial)
	require.Equal(t, "admin@shagan.test", change.ChangedBy)
	require.True(t, org.PlanExpiresAt.Equal(*change.ExpiresAt))

	require.Equal(t, "subscription_trial_started", entry.Action)
	require.Equal(t, "organization_subscription", entry.Entity)
	require.Equal(t, uint(41), entry.OrgID)
}

func TestService_CreateAccount_WithoutChangedBy_RecordsTheSystem(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().CreateOrganization(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, o *Organization) { o.ID = 41 }).Return(nil).Once()
	var change *subscription.OrgPlanChange
	fx.subs.EXPECT().RecordChange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, c *subscription.OrgPlanChange) { change = c }).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().ProvisionDefaultRoles(mock.Anything, uint(41)).Return(nil).Once()
	fx.repo.EXPECT().CreateUser(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, u *User) { u.ID = 1 }).Return(nil).Twice()

	_, err := fx.svc.CreateAccount(context.Background(), CreateAccountInput{
		OrganizationName: "New Org", OwnerEmail: "o@new.test", OwnerPassword: "correct horse battery",
		ServiceCenterEmail: "sc@new.test", ServiceCenterPassword: "another strong password",
	})

	require.NoError(t, err)
	require.Equal(t, "system", change.ChangedBy)
}

// ---- /me and the organization list ----

func TestService_GetMe_CarriesTheOrganizationsSubscription(t *testing.T) {
	fx := newSubsFixture(t)
	fx.repo.EXPECT().GetUserByID(mock.Anything, uint(42)).Return(&User{ID: 42, OrgID: 7}, nil).Once()
	want := &subscription.Summary{
		Entitlements: subscription.Entitlements{Plan: "pro", Trial: true, Features: []string{"customer_display"}},
		Usage:        subscription.Usage{Branches: 2, Devices: 3},
	}
	fx.subs.EXPECT().Summary(mock.Anything, uint(7), mock.Anything).Return(want, nil).Once()

	got, err := fx.svc.GetMe(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, uint(42), got.User.ID)
	require.Same(t, want, got.Subscription)
}

func TestService_ListOrganizations_FlagsOverLimitAndShowsTheEffectivePlan(t *testing.T) {
	fx := newSubsFixture(t)
	lapsed := time.Now().Add(-30 * 24 * time.Hour)
	fx.repo.EXPECT().ListOrganizations(mock.Anything).Return([]Organization{
		{ID: 1, Name: "Basic, within", Plan: "basic"},
		{ID: 2, Name: "Basic, over", Plan: "basic"},
		{ID: 3, Name: "Pro, 2 branches", Plan: "pro"},
		{ID: 4, Name: "Pro that lapsed, 2 branches", Plan: "pro", PlanExpiresAt: &lapsed},
		{ID: 5, Name: "Enterprise, lots", Plan: "enterprise"},
		{ID: 6, Name: "Basic + 1 extra, 2 branches", Plan: "basic", ExtraBranches: 1},
	}, nil).Once()
	fx.subs.EXPECT().UsageByOrg(mock.Anything).Return(map[uint]subscription.Usage{
		1: {Branches: 1, Devices: 2}, 2: {Branches: 3, Devices: 0}, 3: {Branches: 2, Devices: 3},
		4: {Branches: 2, Devices: 3}, 5: {Branches: 40, Devices: 90}, 6: {Branches: 2, Devices: 1},
	}, nil).Once()

	got, err := fx.svc.ListOrganizations(context.Background())

	require.NoError(t, err)
	over := map[uint]bool{}
	effective := map[uint]string{}
	for _, o := range got {
		over[o.ID], effective[o.ID] = o.OverLimit, o.EffectivePlan
	}
	require.Equal(t, map[uint]bool{1: false, 2: true, 3: false, 4: true, 5: false, 6: false}, over)
	require.Equal(t, "basic", effective[4], "a Pro that lapsed past its grace is effectively Basic")
	require.Equal(t, "pro", effective[3])
	require.Equal(t, "pro", got[3].Plan, "the stored plan is still shown")
}

func TestService_GetOrganization_CarriesPlanFieldsAndOverLimit(t *testing.T) {
	fx := newSubsFixture(t)
	end := time.Now().Add(24 * time.Hour)
	fx.repo.EXPECT().GetOrganization(mock.Anything, uint(3)).
		Return(&Organization{ID: 3, Name: "Acme", Plan: "pro", PlanTrial: true, PlanExpiresAt: &end}, nil).Once()
	fx.subs.EXPECT().UsageByOrg(mock.Anything).Return(map[uint]subscription.Usage{3: {Branches: 6}}, nil).Once()

	got, err := fx.svc.GetOrganization(context.Background(), 3)

	require.NoError(t, err)
	require.Equal(t, "pro", got.Plan)
	require.True(t, got.PlanTrial)
	require.Equal(t, "pro", got.EffectivePlan)
	require.True(t, got.OverLimit, "6 branches on Pro's 5")
}
