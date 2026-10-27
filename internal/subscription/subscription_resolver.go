package subscription

import "time"

// Entitlements is what an organization may do right now: the EFFECTIVE plan
// (after expiry and grace), its features (after overrides) and its limits
// (the plan's plus the org's extras). Plan is never the raw stored plan when
// that has lapsed - an expired Pro reads "basic".
type Entitlements struct {
	Plan string `json:"plan"`
	// Trial is true only while the org is actually on its trial.
	Trial bool `json:"trial"`
	// ExpiresAt is the stored plan_expires_at (nil = no end date).
	ExpiresAt *time.Time `json:"expires_at"`
	// InGrace: a paid plan past its end date but still inside the grace period -
	// everything still works, the app should nag.
	InGrace bool `json:"in_grace"`
	// Expired: the stored plan has lapsed and the org has dropped to Basic.
	Expired  bool     `json:"expired"`
	Features []string `json:"features"`
	Limits   Limits   `json:"limits"`
}

// Has reports whether the feature is in the effective feature set.
func (e *Entitlements) Has(feature string) bool {
	for _, f := range e.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// Summary is Entitlements plus live usage - what GET /me carries.
type Summary struct {
	Entitlements
	Usage Usage `json:"usage"`
}

// Exceeded reports whether usage is over either limit (an org that was
// downgraded keeps what it has; it just can't add more).
func (l Limits) Exceeded(u Usage) bool {
	return (l.Branches != nil && u.Branches > *l.Branches) || (l.Devices != nil && u.Devices > *l.Devices)
}

// Resolve is the one rule that turns an org's stored plan block, its feature
// overrides and the clock into Entitlements. Pure - everything that needs the
// answer (RequireFeature, /me, /sync/catalog, the limit checks, the internal
// portal) goes through it, so they can't disagree.
//
//  1. Start from the stored plan (an unknown code is treated as Basic).
//  2. If plan_expires_at has passed: a paid plan keeps working for GracePeriod
//     with InGrace set, then drops to Basic; a trial drops at once. Basic has
//     no end date to honour. Worked out here, on read - no cron job.
//  3. Apply the feature overrides (unknown feature codes are ignored).
//  4. Limits = the effective plan's limit + the org's extras (unlimited stays
//     unlimited).
func Resolve(p OrgPlan, overrides []OrgFeatureOverride, now time.Time) Entitlements {
	plan, ok := LookupPlan(p.Plan)
	if !ok {
		plan, _ = LookupPlan(PlanBasic)
	}
	ent := Entitlements{Plan: plan.Code, Trial: p.Trial, ExpiresAt: p.ExpiresAt}

	if plan.Code != PlanBasic && p.ExpiresAt != nil && !now.Before(*p.ExpiresAt) {
		if !p.Trial && now.Before(p.ExpiresAt.Add(GracePeriod)) {
			ent.InGrace = true
		} else {
			plan, _ = LookupPlan(PlanBasic)
			ent.Plan, ent.Trial, ent.Expired = plan.Code, false, true
		}
	}

	enabled := map[string]bool{}
	for _, f := range plan.Features {
		enabled[f] = true
	}
	for _, o := range overrides {
		if IsFeature(o.Feature) {
			enabled[o.Feature] = o.Enabled
		}
	}
	ent.Features = []string{}
	for _, f := range allFeatures {
		if enabled[f] {
			ent.Features = append(ent.Features, f)
		}
	}

	ent.Limits = Limits{Branches: addExtra(plan.Limits.Branches, p.ExtraBranches), Devices: addExtra(plan.Limits.Devices, p.ExtraDevices)}
	return ent
}

func addExtra(limit *int, extra int) *int {
	if limit == nil {
		return nil
	}
	n := *limit + extra
	return &n
}

// NewTrial is the plan block a brand-new organization starts with: the Pro
// trial, ending TrialDuration from now.
func NewTrial(now time.Time) OrgPlan {
	end := now.Add(TrialDuration)
	return OrgPlan{Plan: TrialPlan, ExpiresAt: &end, Trial: true}
}
