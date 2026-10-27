package subscription

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
)

type fakeTransactioner struct{}

func (fakeTransactioner) Transaction(fc func(tx *gorm.DB) error, _ ...*sql.TxOptions) error {
	return fc(nil)
}

type fixture struct {
	svc   *Service
	repo  *MockRepository
	audW  *MockAuditWriter
	clock *time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo, audW := NewMockRepository(t), NewMockAuditWriter(t)
	now := t0
	svc := NewService(repo, audW, fakeTransactioner{})
	svc.clock = func() time.Time { return now }
	fx := &fixture{svc: svc, repo: repo, audW: audW, clock: &now}
	return fx
}

func requireCoded(t *testing.T, err error, status int, code string) common.RestError {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
	require.Equal(t, code, restErr.Code)
	return restErr
}

func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

// ---- the cache ----

func (fx *fixture) expectOnePlanRead(plan OrgPlan) {
	fx.repo.EXPECT().GetOrgPlan(mock.Anything, uint(7)).Return(&plan, nil).Once()
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return(nil, nil).Once()
}

func TestEntitlements_AreCachedPerOrgForAShortTime(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanPro})

	first, err := fx.svc.Entitlements(context.Background(), 7, t0)
	require.NoError(t, err)
	*fx.clock = t0.Add(EntitlementsTTL - time.Second)
	second, err := fx.svc.Entitlements(context.Background(), 7, *fx.clock) // no further repo call: the mock would fail

	require.NoError(t, err)
	require.Equal(t, PlanPro, first.Plan)
	require.Equal(t, first, second)
}

func TestEntitlements_AreReadAgainAfterTheTTL(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanBasic})
	_, err := fx.svc.Entitlements(context.Background(), 7, t0)
	require.NoError(t, err)

	*fx.clock = t0.Add(EntitlementsTTL + time.Second)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanPro}) // the plan changed meanwhile
	got, err := fx.svc.Entitlements(context.Background(), 7, *fx.clock)

	require.NoError(t, err)
	require.Equal(t, PlanPro, got.Plan)
}

func TestEntitlements_OneOrgsCacheDoesNotServeAnother(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanPro})
	fx.repo.EXPECT().GetOrgPlan(mock.Anything, uint(8)).Return(&OrgPlan{Plan: PlanBasic}, nil).Once()
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(8)).Return(nil, nil).Once()

	a, err := fx.svc.Entitlements(context.Background(), 7, t0)
	require.NoError(t, err)
	b, err := fx.svc.Entitlements(context.Background(), 8, t0)
	require.NoError(t, err)

	require.Equal(t, PlanPro, a.Plan)
	require.Equal(t, PlanBasic, b.Plan)
}

func TestEntitlements_UnknownOrg_IsNotFoundAndNotCached(t *testing.T) {
	fx := newFixture(t)
	fx.repo.EXPECT().GetOrgPlan(mock.Anything, uint(99)).Return(nil, common.NotFoundError("organization not found")).Twice()

	_, err := fx.svc.Entitlements(context.Background(), 99, t0)
	requireStatus(t, err, http.StatusNotFound)
	_, err = fx.svc.Entitlements(context.Background(), 99, t0)
	requireStatus(t, err, http.StatusNotFound)
}

// An /internal write must show at once, not after the TTL.
func TestUpdateSubscription_ClearsTheCache(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanBasic})
	_, err := fx.svc.Entitlements(context.Background(), 7, t0)
	require.NoError(t, err)

	fx.expectUpdate(OrgPlan{Plan: PlanBasic})
	fx.repo.EXPECT().UpdateOrgPlan(mock.Anything, uint(7), mock.Anything).Return(nil).Once()
	fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	fx.expectDetailReads(OrgPlan{Plan: PlanPro})
	_, err = fx.svc.UpdateSubscription(context.Background(), 7, UpdateSubscriptionRequest{Plan: strp(PlanPro), ChangedBy: "admin@shagan.test"})
	require.NoError(t, err)

	fx.expectOnePlanRead(OrgPlan{Plan: PlanPro}) // read again: the cache entry is gone
	got, err := fx.svc.Entitlements(context.Background(), 7, t0)
	require.NoError(t, err)
	require.Equal(t, PlanPro, got.Plan)
}

func TestFeatureOverrideWrites_ClearTheCache(t *testing.T) {
	for name, write := range map[string]func(fx *fixture) error{
		"set": func(fx *fixture) error {
			fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return(nil, nil).Once()
			fx.repo.EXPECT().UpsertFeatureOverride(mock.Anything, mock.Anything).Return(nil).Once()
			fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Return(nil).Once()
			fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
			fx.expectDetailReads(OrgPlan{Plan: PlanBasic})
			_, err := fx.svc.SetFeatureOverride(context.Background(), 7, FeatureLoyalty, FeatureOverrideRequest{Enabled: boolp(true), ChangedBy: "a@shagan.test"})
			return err
		},
		"clear": func(fx *fixture) error {
			fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return([]OrgFeatureOverride{{OrgID: 7, Feature: FeatureLoyalty, Enabled: true}}, nil).Once()
			fx.repo.EXPECT().DeleteFeatureOverride(mock.Anything, uint(7), FeatureLoyalty).Return(true, nil).Once()
			fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Return(nil).Once()
			fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
			fx.expectDetailReads(OrgPlan{Plan: PlanBasic})
			_, err := fx.svc.ClearFeatureOverride(context.Background(), 7, FeatureLoyalty, FeatureOverrideRequest{ChangedBy: "a@shagan.test"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			fx.expectOnePlanRead(OrgPlan{Plan: PlanBasic})
			_, err := fx.svc.Entitlements(context.Background(), 7, t0)
			require.NoError(t, err)
			fx.repo.EXPECT().LockOrgPlan(mock.Anything, uint(7)).Return(&OrgPlan{Plan: PlanBasic}, nil).Once()

			require.NoError(t, write(fx))

			fx.expectOnePlanRead(OrgPlan{Plan: PlanBasic}) // read again: the cache entry is gone
			_, err = fx.svc.Entitlements(context.Background(), 7, t0)
			require.NoError(t, err)
		})
	}
}

// ---- the limit check ----

func (fx *fixture) lockPlan(p OrgPlan) {
	fx.repo.EXPECT().LockOrgPlan(mock.Anything, uint(7)).Return(&p, nil).Once()
}

func TestEnforceLimit_BelowTheLimit_Passes(t *testing.T) {
	fx := newFixture(t)
	fx.lockPlan(OrgPlan{Plan: PlanPro})
	fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(4, nil).Once()

	require.NoError(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0))
}

func TestEnforceLimit_AtTheLimit_IsRefusedWithPlanLimitReached(t *testing.T) {
	cases := map[string]struct {
		kind     LimitKind
		plan     OrgPlan
		count    func(fx *fixture)
		max, cur int
	}{
		"branches on basic": {LimitBranches, OrgPlan{Plan: PlanBasic}, func(fx *fixture) {
			fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(1, nil).Once()
		}, 1, 1},
		"devices on basic": {LimitDevices, OrgPlan{Plan: PlanBasic}, func(fx *fixture) {
			fx.repo.EXPECT().CountActiveDevices(mock.Anything, uint(7)).Return(2, nil).Once()
		}, 2, 2},
		"branches on pro": {LimitBranches, OrgPlan{Plan: PlanPro}, func(fx *fixture) {
			fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(5, nil).Once()
		}, 5, 5},
		"an over-limit org (after a downgrade) can't add more": {LimitBranches, OrgPlan{Plan: PlanBasic}, func(fx *fixture) {
			fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(3, nil).Once()
		}, 1, 3},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			fx.lockPlan(c.plan)
			c.count(fx)

			err := fx.svc.EnforceLimit(nil, 7, c.kind, t0)

			restErr := requireCoded(t, err, http.StatusForbidden, "plan_limit_reached")
			require.Equal(t, string(c.kind), restErr.Details["limit"])
			require.Equal(t, c.max, restErr.Details["max"])
			require.Equal(t, c.cur, restErr.Details["current"])
		})
	}
}

func TestEnforceLimit_ExtrasRaiseTheLimit(t *testing.T) {
	fx := newFixture(t)
	fx.lockPlan(OrgPlan{Plan: PlanPro, ExtraBranches: 1}) // 5 + 1
	fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(5, nil).Once()
	require.NoError(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0))

	fx.lockPlan(OrgPlan{Plan: PlanPro, ExtraBranches: 1})
	fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(6, nil).Once()
	restErr := requireCoded(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0), http.StatusForbidden, "plan_limit_reached")
	require.Equal(t, 6, restErr.Details["max"])
}

func TestEnforceLimit_Unlimited_NeverCounts(t *testing.T) {
	fx := newFixture(t)
	fx.lockPlan(OrgPlan{Plan: PlanEnterprise})
	// no Count... expectation: counting an unlimited plan would fail the test

	require.NoError(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0))
}

// An expired plan counts as Basic when the limit is checked.
func TestEnforceLimit_UsesTheEffectivePlan_AfterExpiry(t *testing.T) {
	fx := newFixture(t)
	fx.lockPlan(OrgPlan{Plan: PlanPro, ExpiresAt: at(-30 * 24 * time.Hour)})
	fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(1, nil).Once()

	requireCoded(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0), http.StatusForbidden, "plan_limit_reached")
}

// Inside the grace period the paid limits still apply.
func TestEnforceLimit_UsesThePaidPlan_InsideTheGracePeriod(t *testing.T) {
	fx := newFixture(t)
	fx.lockPlan(OrgPlan{Plan: PlanPro, ExpiresAt: at(-2 * 24 * time.Hour)})
	fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(3, nil).Once()

	require.NoError(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0))
}

// The check reads the stored plan under a lock (LockOrgPlan), never the cache:
// the lock is what stops two parallel creates both passing.
func TestEnforceLimit_LocksTheOrganizationAndIgnoresTheCache(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanEnterprise}) // cached as unlimited...
	_, err := fx.svc.Entitlements(context.Background(), 7, t0)
	require.NoError(t, err)

	fx.lockPlan(OrgPlan{Plan: PlanBasic}) // ...but the stored plan was just downgraded
	fx.repo.EXPECT().CountActiveBranches(mock.Anything, uint(7)).Return(1, nil).Once()

	requireCoded(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0), http.StatusForbidden, "plan_limit_reached")
}

func TestEnforceLimit_UnknownOrg_IsNotFound(t *testing.T) {
	fx := newFixture(t)
	fx.repo.EXPECT().LockOrgPlan(mock.Anything, uint(7)).Return(nil, common.NotFoundError("organization not found")).Once()

	requireStatus(t, fx.svc.EnforceLimit(nil, 7, LimitBranches, t0), http.StatusNotFound)
}

// ---- GET /me's summary ----

func TestSummary_CarriesLiveUsage(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanPro, Trial: true, ExpiresAt: at(24 * time.Hour)})
	fx.repo.EXPECT().ActiveUsage(mock.Anything, uint(7)).Return(Usage{Branches: 2, Devices: 3}, nil).Once()

	got, err := fx.svc.Summary(context.Background(), 7, t0)

	require.NoError(t, err)
	require.Equal(t, PlanPro, got.Plan)
	require.True(t, got.Trial)
	require.Equal(t, Usage{Branches: 2, Devices: 3}, got.Usage)
	body, err := json.Marshal(got)
	require.NoError(t, err)
	var shape map[string]any
	require.NoError(t, json.Unmarshal(body, &shape))
	for _, key := range []string{"plan", "trial", "expires_at", "in_grace", "expired", "features", "limits", "usage"} {
		require.Contains(t, shape, key)
	}
	require.Equal(t, map[string]any{"branches": float64(5), "devices": float64(10)}, shape["limits"])
	require.Equal(t, map[string]any{"branches": float64(2), "devices": float64(3)}, shape["usage"])
}

func TestSummary_UnlimitedLimitsSerializeAsNull(t *testing.T) {
	fx := newFixture(t)
	fx.expectOnePlanRead(OrgPlan{Plan: PlanEnterprise})
	fx.repo.EXPECT().ActiveUsage(mock.Anything, uint(7)).Return(Usage{}, nil).Once()
	got, err := fx.svc.Summary(context.Background(), 7, t0)
	require.NoError(t, err)

	body, err := json.Marshal(got)
	require.NoError(t, err)
	var shape map[string]any
	require.NoError(t, json.Unmarshal(body, &shape))
	require.Equal(t, map[string]any{"branches": nil, "devices": nil}, shape["limits"])
}

// ---- PATCH /internal/organizations/:id/subscription ----

func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }
func intp2(n int) *int      { return &n }

func (fx *fixture) expectUpdate(before OrgPlan) {
	fx.repo.EXPECT().LockOrgPlan(mock.Anything, uint(7)).Return(&before, nil).Once()
}

// expectDetailReads is what GetSubscription reads after a write.
func (fx *fixture) expectDetailReads(plan OrgPlan) {
	fx.repo.EXPECT().GetOrgPlan(mock.Anything, uint(7)).Return(&plan, nil).Once()
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return(nil, nil).Once()
	fx.repo.EXPECT().ActiveUsage(mock.Anything, uint(7)).Return(Usage{}, nil).Once()
	fx.repo.EXPECT().ListPlanChanges(mock.Anything, uint(7), HistoryLimit).Return(nil, nil).Once()
}

func TestUpdateSubscription_WritesThePlanAHistoryRowAndAnAuditEntry(t *testing.T) {
	fx := newFixture(t)
	end := t0.Add(30 * 24 * time.Hour)
	fx.expectUpdate(OrgPlan{Plan: PlanBasic})
	var written OrgPlan
	fx.repo.EXPECT().UpdateOrgPlan(mock.Anything, uint(7), mock.Anything).Run(func(_ *gorm.DB, _ uint, p OrgPlan) { written = p }).Return(nil).Once()
	var change *OrgPlanChange
	fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, c *OrgPlanChange) { change = c }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()
	fx.expectDetailReads(OrgPlan{Plan: PlanPro, ExpiresAt: &end, ExtraBranches: 1})

	got, err := fx.svc.UpdateSubscription(context.Background(), 7, UpdateSubscriptionRequest{
		Plan: strp(PlanPro), ExpiresAt: NullableTime{Set: true, Time: &end}, Trial: boolp(false), ExtraBranches: intp2(1),
		Note: "paid, invoice 114", ChangedBy: " admin@shagan.test ",
	})

	require.NoError(t, err)
	require.Equal(t, uint(7), got.OrganizationID)
	require.Equal(t, OrgPlan{Plan: PlanPro, ExpiresAt: &end, Trial: false, ExtraBranches: 1, ExtraDevices: 0}, written)

	require.NotNil(t, change)
	require.Equal(t, uint(7), change.OrgID)
	require.Equal(t, PlanBasic, change.FromPlan)
	require.Equal(t, PlanPro, change.ToPlan)
	require.Equal(t, end, *change.ExpiresAt)
	require.Equal(t, 1, change.ExtraBranches)
	require.Equal(t, "paid, invoice 114", change.Note)
	require.Equal(t, "admin@shagan.test", change.ChangedBy, "trimmed")

	require.NotNil(t, entry)
	require.Equal(t, uint(7), entry.OrgID)
	require.Equal(t, "organization_subscription", entry.Entity)
	require.Equal(t, "7", entry.EntityID)
	require.Equal(t, "subscription_updated", entry.Action)
	require.Contains(t, string(entry.Before), `"plan":"basic"`)
	require.Contains(t, string(entry.After), `"plan":"pro"`)
	require.Contains(t, string(entry.After), `"changed_by":"admin@shagan.test"`)
}

// Omitted fields stay as they are; "expires_at": null clears the end date.
func TestUpdateSubscription_OmittedFieldsAreKept_NullClearsTheEndDate(t *testing.T) {
	fx := newFixture(t)
	end := t0.Add(24 * time.Hour)
	fx.expectUpdate(OrgPlan{Plan: PlanPro, ExpiresAt: &end, Trial: true, ExtraDevices: 3})
	var written OrgPlan
	fx.repo.EXPECT().UpdateOrgPlan(mock.Anything, uint(7), mock.Anything).Run(func(_ *gorm.DB, _ uint, p OrgPlan) { written = p }).Return(nil).Once()
	fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Return(nil).Once()
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
	fx.expectDetailReads(written)

	_, err := fx.svc.UpdateSubscription(context.Background(), 7, UpdateSubscriptionRequest{
		ExpiresAt: NullableTime{Set: true}, Trial: boolp(false), ChangedBy: "a@shagan.test",
	})

	require.NoError(t, err)
	require.Equal(t, OrgPlan{Plan: PlanPro, ExpiresAt: nil, Trial: false, ExtraDevices: 3}, written)
}

func TestUpdateSubscription_NothingChanged_WritesNothing(t *testing.T) {
	fx := newFixture(t)
	fx.expectUpdate(OrgPlan{Plan: PlanPro, ExtraBranches: 1})
	fx.expectDetailReads(OrgPlan{Plan: PlanPro, ExtraBranches: 1})
	// no UpdateOrgPlan, history or audit: the mocks would fail the test

	_, err := fx.svc.UpdateSubscription(context.Background(), 7, UpdateSubscriptionRequest{Plan: strp(PlanPro), ExtraBranches: intp2(1), ChangedBy: "a@shagan.test"})

	require.NoError(t, err)
}

func TestUpdateSubscription_Validation_RejectsBeforeAnyRead(t *testing.T) {
	cases := map[string]UpdateSubscriptionRequest{
		"unknown plan":       {Plan: strp("platinum"), ChangedBy: "a@shagan.test"},
		"negative branches":  {ExtraBranches: intp2(-1), ChangedBy: "a@shagan.test"},
		"negative devices":   {ExtraDevices: intp2(-1), ChangedBy: "a@shagan.test"},
		"missing changed_by": {Plan: strp(PlanPro)},
		"blank changed_by":   {Plan: strp(PlanPro), ChangedBy: "   "},
		"note too long":      {Plan: strp(PlanPro), ChangedBy: "a@shagan.test", Note: string(make([]rune, 501))},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t) // no repo call at all
			if name == "note too long" {
				in.Note = ""
				for i := 0; i < 501; i++ {
					in.Note += "x"
				}
			}

			_, err := fx.svc.UpdateSubscription(context.Background(), 7, in)

			requireStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestUpdateSubscription_UnknownOrg_IsNotFound(t *testing.T) {
	fx := newFixture(t)
	fx.repo.EXPECT().LockOrgPlan(mock.Anything, uint(7)).Return(nil, common.NotFoundError("organization not found")).Once()

	_, err := fx.svc.UpdateSubscription(context.Background(), 7, UpdateSubscriptionRequest{Plan: strp(PlanPro), ChangedBy: "a@shagan.test"})

	requireStatus(t, err, http.StatusNotFound)
}

// ---- the detail ----

func TestGetSubscription_IsEffectiveRawOverridesHistoryAndOverLimit(t *testing.T) {
	fx := newFixture(t)
	fx.clock = nil
	end := t0.Add(-30 * 24 * time.Hour) // Pro that lapsed: effectively Basic
	fx.repo.EXPECT().GetOrgPlan(mock.Anything, uint(7)).Return(&OrgPlan{Plan: PlanPro, ExpiresAt: &end}, nil).Once()
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return([]OrgFeatureOverride{{OrgID: 7, Feature: FeatureLoyalty, Enabled: true}}, nil).Once()
	fx.repo.EXPECT().ActiveUsage(mock.Anything, uint(7)).Return(Usage{Branches: 3, Devices: 1}, nil).Once()
	fx.repo.EXPECT().ListPlanChanges(mock.Anything, uint(7), HistoryLimit).Return([]OrgPlanChange{{ID: 2, OrgID: 7, ToPlan: PlanPro}, {ID: 1, OrgID: 7, ToPlan: PlanBasic}}, nil).Once()
	fx.svc.clock = func() time.Time { return t0 }

	got, err := fx.svc.GetSubscription(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, PlanBasic, got.Effective.Plan)
	require.True(t, got.Effective.Expired)
	require.Equal(t, []string{FeatureLoyalty}, got.Effective.Features)
	require.Equal(t, Usage{Branches: 3, Devices: 1}, got.Effective.Usage)
	require.Equal(t, PlanPro, got.Raw.Plan, "the raw stored plan, not the effective one")
	require.True(t, got.OverLimit, "3 active branches on Basic's 1")
	require.Len(t, got.Overrides, 1)
	require.Len(t, got.History, 2)
}

func TestGetSubscription_EmptyListsAreNotNull(t *testing.T) {
	fx := newFixture(t)
	fx.expectDetailReads(OrgPlan{Plan: PlanBasic})

	got, err := fx.svc.GetSubscription(context.Background(), 7)

	require.NoError(t, err)
	body, err := json.Marshal(got)
	require.NoError(t, err)
	var shape map[string]any
	require.NoError(t, json.Unmarshal(body, &shape))
	require.Equal(t, []any{}, shape["overrides"])
	require.Equal(t, []any{}, shape["history"])
	require.False(t, got.OverLimit)
}

// ---- feature overrides ----

func TestSetFeatureOverride_UpsertsAndRecordsHistoryAndAudit(t *testing.T) {
	fx := newFixture(t)
	fx.expectUpdate(OrgPlan{Plan: PlanBasic, ExtraBranches: 2})
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return([]OrgFeatureOverride{{OrgID: 7, Feature: FeatureLoyalty, Enabled: false}}, nil).Once()
	var saved *OrgFeatureOverride
	fx.repo.EXPECT().UpsertFeatureOverride(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, o *OrgFeatureOverride) { saved = o }).Return(nil).Once()
	var change *OrgPlanChange
	fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, c *OrgPlanChange) { change = c }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()
	fx.expectDetailReads(OrgPlan{Plan: PlanBasic, ExtraBranches: 2})

	_, err := fx.svc.SetFeatureOverride(context.Background(), 7, FeatureLoyalty, FeatureOverrideRequest{Enabled: boolp(true), ChangedBy: "a@shagan.test", Note: "beta"})

	require.NoError(t, err)
	require.Equal(t, OrgFeatureOverride{OrgID: 7, Feature: FeatureLoyalty, Enabled: true}, *saved)
	require.Equal(t, PlanBasic, change.FromPlan)
	require.Equal(t, PlanBasic, change.ToPlan, "the plan itself didn't change")
	require.Equal(t, 2, change.ExtraBranches, "the stored plan fields are repeated")
	require.Contains(t, change.Note, "loyalty")
	require.Contains(t, change.Note, "on")
	require.Contains(t, change.Note, "beta")
	require.Equal(t, "a@shagan.test", change.ChangedBy)
	require.Equal(t, "feature_override_set", entry.Action)
	require.Contains(t, string(entry.Before), `"enabled":false`)
	require.Contains(t, string(entry.After), `"enabled":true`)
}

func TestSetFeatureOverride_Validation(t *testing.T) {
	cases := map[string]struct {
		feature string
		in      FeatureOverrideRequest
	}{
		"unknown feature":    {"time_travel", FeatureOverrideRequest{Enabled: boolp(true), ChangedBy: "a@shagan.test"}},
		"enabled is missing": {FeatureLoyalty, FeatureOverrideRequest{ChangedBy: "a@shagan.test"}},
		"changed_by missing": {FeatureLoyalty, FeatureOverrideRequest{Enabled: boolp(true)}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)

			_, err := fx.svc.SetFeatureOverride(context.Background(), 7, c.feature, c.in)

			requireStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestClearFeatureOverride_RemovesItAndRecordsHistoryAndAudit(t *testing.T) {
	fx := newFixture(t)
	fx.expectUpdate(OrgPlan{Plan: PlanPro})
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return([]OrgFeatureOverride{{OrgID: 7, Feature: FeaturePromotions, Enabled: false}}, nil).Once()
	fx.repo.EXPECT().DeleteFeatureOverride(mock.Anything, uint(7), FeaturePromotions).Return(true, nil).Once()
	var change *OrgPlanChange
	fx.repo.EXPECT().CreatePlanChange(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, c *OrgPlanChange) { change = c }).Return(nil).Once()
	var entry *audit.AuditLog
	fx.audW.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Run(func(_ *gorm.DB, e *audit.AuditLog) { entry = e }).Return(nil).Once()
	fx.expectDetailReads(OrgPlan{Plan: PlanPro})

	_, err := fx.svc.ClearFeatureOverride(context.Background(), 7, FeaturePromotions, FeatureOverrideRequest{ChangedBy: "a@shagan.test"})

	require.NoError(t, err)
	require.Contains(t, change.Note, "promotions")
	require.Equal(t, "feature_override_cleared", entry.Action)
	require.Contains(t, string(entry.Before), `"enabled":false`)
}

func TestClearFeatureOverride_NoOverride_IsANoOp(t *testing.T) {
	fx := newFixture(t)
	fx.expectUpdate(OrgPlan{Plan: PlanPro})
	fx.repo.EXPECT().ListFeatureOverrides(mock.Anything, uint(7)).Return(nil, nil).Once()
	fx.repo.EXPECT().DeleteFeatureOverride(mock.Anything, uint(7), FeaturePromotions).Return(false, nil).Once()
	fx.expectDetailReads(OrgPlan{Plan: PlanPro})
	// no history, no audit

	_, err := fx.svc.ClearFeatureOverride(context.Background(), 7, FeaturePromotions, FeatureOverrideRequest{ChangedBy: "a@shagan.test"})

	require.NoError(t, err)
}

func TestClearFeatureOverride_UnknownFeature_IsBadRequest(t *testing.T) {
	fx := newFixture(t)

	_, err := fx.svc.ClearFeatureOverride(context.Background(), 7, "time_travel", FeatureOverrideRequest{ChangedBy: "a@shagan.test"})

	requireStatus(t, err, http.StatusBadRequest)
}

// ---- JSON: omitted vs null ----

func TestNullableTime_TellsOmittedFromNull(t *testing.T) {
	var omitted, null, set UpdateSubscriptionRequest
	require.NoError(t, json.Unmarshal([]byte(`{"plan":"pro"}`), &omitted))
	require.NoError(t, json.Unmarshal([]byte(`{"expires_at":null}`), &null))
	require.NoError(t, json.Unmarshal([]byte(`{"expires_at":"2026-12-01T00:00:00Z"}`), &set))

	require.False(t, omitted.ExpiresAt.Set)
	require.True(t, null.ExpiresAt.Set)
	require.Nil(t, null.ExpiresAt.Time)
	require.True(t, set.ExpiresAt.Set)
	require.Equal(t, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), *set.ExpiresAt.Time)
	require.Error(t, json.Unmarshal([]byte(`{"expires_at":"not a date"}`), &UpdateSubscriptionRequest{}))
}

func TestErrors_HaveTheDocumentedShape(t *testing.T) {
	limit := LimitReachedError(LimitBranches, 1, 1)
	require.Equal(t, http.StatusForbidden, limit.Status)
	require.Equal(t, "plan_limit_reached", limit.Code)
	require.Equal(t, map[string]any{"limit": "branches", "max": 1, "current": 1}, limit.Details)

	feat := FeatureNotInPlanError(FeaturePromotions, PlanBasic)
	require.Equal(t, http.StatusForbidden, feat.Status)
	require.Equal(t, "feature_not_in_plan", feat.Code)
	require.Equal(t, map[string]any{"feature": "promotions", "plan": "basic"}, feat.Details)
}
