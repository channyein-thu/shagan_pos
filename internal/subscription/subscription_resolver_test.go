package subscription

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func ip(n int) *int { return &n }

func at(d time.Duration) *time.Time {
	t := t0.Add(d)
	return &t
}

func requireLimit(t *testing.T, want *int, got *int, what string) {
	t.Helper()
	if want == nil {
		require.Nil(t, got, "%s should be unlimited", what)
		return
	}
	require.NotNil(t, got, "%s should be %d, not unlimited", what, *want)
	require.Equal(t, *want, *got, what)
}

// ---- each tier ----

func TestResolve_EachTier(t *testing.T) {
	cases := map[string]struct {
		plan             string
		features         []string
		branches, device *int
	}{
		"basic":      {PlanBasic, []string{}, ip(1), ip(2)},
		"pro":        {PlanPro, []string{"customer_display", "advanced_reports", "report_export", "promotions"}, ip(5), ip(10)},
		"enterprise": {PlanEnterprise, []string{"customer_display", "advanced_reports", "report_export", "promotions", "loyalty", "sms_receipts", "api_access"}, nil, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ent := Resolve(OrgPlan{Plan: c.plan}, nil, t0)

			require.Equal(t, c.plan, ent.Plan)
			require.Equal(t, c.features, ent.Features)
			requireLimit(t, c.branches, ent.Limits.Branches, "branches")
			requireLimit(t, c.device, ent.Limits.Devices, "devices")
			require.False(t, ent.InGrace)
			require.False(t, ent.Expired)
		})
	}
}

func TestResolve_UnknownStoredPlan_IsBasic(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: "platinum"}, nil, t0)
	require.Equal(t, PlanBasic, ent.Plan)
	require.Equal(t, 1, *ent.Limits.Branches)
}

func TestResolve_FeaturesAreNeverNull(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanBasic}, nil, t0)
	require.NotNil(t, ent.Features)
	require.Empty(t, ent.Features)
}

// ---- expiry, grace and the trial ----

func TestResolve_PaidPlanBeforeItsEndDate_IsUnaffected(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, ExpiresAt: at(24 * time.Hour)}, nil, t0)
	require.Equal(t, PlanPro, ent.Plan)
	require.False(t, ent.InGrace)
	require.False(t, ent.Expired)
}

func TestResolve_NoEndDate_NeverExpires(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanEnterprise}, nil, t0.Add(10*365*24*time.Hour))
	require.Equal(t, PlanEnterprise, ent.Plan)
	require.False(t, ent.Expired)
}

// A paid plan keeps working for seven days after it ends - flagged in_grace.
func TestResolve_PaidPlanInsideTheGracePeriod_KeepsItsPlanAndIsInGrace(t *testing.T) {
	for _, past := range []time.Duration{time.Second, 3 * 24 * time.Hour, 7*24*time.Hour - time.Second} {
		ent := Resolve(OrgPlan{Plan: PlanPro, ExpiresAt: at(-past)}, nil, t0)

		require.Equal(t, PlanPro, ent.Plan, "%v past the end", past)
		require.True(t, ent.InGrace, "%v past the end", past)
		require.False(t, ent.Expired)
		require.Equal(t, 5, *ent.Limits.Branches)
		require.Contains(t, ent.Features, FeaturePromotions)
	}
}

// After the grace period the org drops to Basic - and is never suspended.
func TestResolve_PaidPlanAfterTheGracePeriod_DropsToBasic(t *testing.T) {
	for _, past := range []time.Duration{7 * 24 * time.Hour, 30 * 24 * time.Hour} {
		ent := Resolve(OrgPlan{Plan: PlanPro, ExpiresAt: at(-past)}, nil, t0)

		require.Equal(t, PlanBasic, ent.Plan, "%v past the end", past)
		require.True(t, ent.Expired)
		require.False(t, ent.InGrace)
		require.Empty(t, ent.Features)
		require.Equal(t, 1, *ent.Limits.Branches)
		require.Equal(t, 2, *ent.Limits.Devices)
	}
}

// The moment of expiry itself starts the grace period.
func TestResolve_AtTheExactEndInstant_IsInGrace(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, ExpiresAt: at(0)}, nil, t0)
	require.True(t, ent.InGrace)
}

func TestResolve_TrialRunning_IsProAndMarkedTrial(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, Trial: true, ExpiresAt: at(5 * 24 * time.Hour)}, nil, t0)
	require.Equal(t, PlanPro, ent.Plan)
	require.True(t, ent.Trial)
	require.False(t, ent.InGrace)
	require.Equal(t, 5, *ent.Limits.Branches)
}

// A trial ends on its date - no grace period - and the org is Basic, no longer a trial.
func TestResolve_TrialEnded_DropsToBasicAtOnceWithNoGrace(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, Trial: true, ExpiresAt: at(-time.Second)}, nil, t0)

	require.Equal(t, PlanBasic, ent.Plan)
	require.False(t, ent.Trial)
	require.True(t, ent.Expired)
	require.False(t, ent.InGrace)
	require.NotNil(t, ent.ExpiresAt, "the stored end date is still reported")
}

func TestResolve_BasicWithAnEndDateInThePast_IsStillJustBasic(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanBasic, ExpiresAt: at(-100 * 24 * time.Hour)}, nil, t0)
	require.Equal(t, PlanBasic, ent.Plan)
	require.False(t, ent.Expired)
}

// ---- feature overrides ----

func TestResolve_FeatureOverrideOn_AddsAFeatureThePlanLacks(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanBasic}, []OrgFeatureOverride{{Feature: FeatureLoyalty, Enabled: true}}, t0)
	require.Equal(t, []string{FeatureLoyalty}, ent.Features)
	require.Equal(t, PlanBasic, ent.Plan, "an override changes the feature, not the plan")
}

func TestResolve_FeatureOverrideOff_RemovesAFeatureThePlanHas(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro}, []OrgFeatureOverride{{Feature: FeaturePromotions, Enabled: false}}, t0)
	require.NotContains(t, ent.Features, FeaturePromotions)
	require.Contains(t, ent.Features, FeatureCustomerDisplay)
}

// Overrides apply after expiry: a beta feature switched on survives the drop to Basic.
func TestResolve_OverridesApplyAfterExpiry(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, ExpiresAt: at(-30 * 24 * time.Hour)},
		[]OrgFeatureOverride{{Feature: FeatureSMSReceipts, Enabled: true}, {Feature: FeatureReportExport, Enabled: false}}, t0)

	require.Equal(t, PlanBasic, ent.Plan)
	require.Equal(t, []string{FeatureSMSReceipts}, ent.Features)
}

func TestResolve_UnknownOverrideCode_IsIgnored(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanBasic}, []OrgFeatureOverride{{Feature: "time_travel", Enabled: true}}, t0)
	require.Empty(t, ent.Features)
}

func TestResolve_FeaturesComeBackInDisplayOrder(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanBasic}, []OrgFeatureOverride{{Feature: FeatureAPIAccess, Enabled: true}, {Feature: FeatureCustomerDisplay, Enabled: true}}, t0)
	require.Equal(t, []string{FeatureCustomerDisplay, FeatureAPIAccess}, ent.Features)
}

// ---- extras and unlimited ----

func TestResolve_ExtrasAreAddedToThePlansLimits(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, ExtraBranches: 1, ExtraDevices: 4}, nil, t0)
	require.Equal(t, 6, *ent.Limits.Branches, "5 + 1")
	require.Equal(t, 14, *ent.Limits.Devices, "10 + 4")

	basic := Resolve(OrgPlan{Plan: PlanBasic, ExtraBranches: 2}, nil, t0)
	require.Equal(t, 3, *basic.Limits.Branches)
	require.Equal(t, 2, *basic.Limits.Devices)
}

func TestResolve_UnlimitedStaysUnlimitedWithExtras(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanEnterprise, ExtraBranches: 5, ExtraDevices: 5}, nil, t0)
	require.Nil(t, ent.Limits.Branches)
	require.Nil(t, ent.Limits.Devices)
}

// Extras are the org's own add-ons and survive a drop to Basic.
func TestResolve_ExtrasSurviveAnExpiredPlan(t *testing.T) {
	ent := Resolve(OrgPlan{Plan: PlanPro, ExpiresAt: at(-30 * 24 * time.Hour), ExtraBranches: 2}, nil, t0)
	require.Equal(t, PlanBasic, ent.Plan)
	require.Equal(t, 3, *ent.Limits.Branches)
}

func TestLimits_Exceeded(t *testing.T) {
	l := Limits{Branches: ip(1), Devices: ip(2)}
	require.False(t, l.Exceeded(Usage{Branches: 1, Devices: 2}), "at the limit is not over it")
	require.True(t, l.Exceeded(Usage{Branches: 2, Devices: 0}))
	require.True(t, l.Exceeded(Usage{Branches: 0, Devices: 3}))
	require.False(t, Limits{}.Exceeded(Usage{Branches: 99, Devices: 99}), "unlimited is never exceeded")
}

func TestNewTrial_IsProForFourteenDays(t *testing.T) {
	trial := NewTrial(t0)
	require.Equal(t, PlanPro, trial.Plan)
	require.True(t, trial.Trial)
	require.Equal(t, t0.Add(14*24*time.Hour), *trial.ExpiresAt)
}

func TestPlanCatalogue_IsTheThreeTiersLowestFirst(t *testing.T) {
	var codes []string
	for _, p := range Plans() {
		codes = append(codes, p.Code)
	}
	require.Equal(t, []string{PlanBasic, PlanPro, PlanEnterprise}, codes)
	// callers get copies: editing one must not edit the catalogue
	Plans()[1].Features[0] = "tampered"
	require.Equal(t, FeatureCustomerDisplay, Plans()[1].Features[0])
}
