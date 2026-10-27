package subscription

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
)

// EntitlementsTTL is how long one org's resolved entitlements are reused.
const EntitlementsTTL = 60 * time.Second

type cacheEntry struct {
	ent     Entitlements
	expires time.Time
}

type Service struct {
	repo  Repository
	audit AuditWriter
	db    common.Transactioner
	clock func() time.Time

	mu    sync.Mutex
	cache map[uint]cacheEntry
}

func NewService(repo Repository, auditWriter AuditWriter, db common.Transactioner) *Service {
	return &Service{repo: repo, audit: auditWriter, db: db, clock: time.Now, cache: map[uint]cacheEntry{}}
}

var _ Interface = (*Service)(nil)

func (s *Service) invalidate(orgID uint) {
	s.mu.Lock()
	delete(s.cache, orgID)
	s.mu.Unlock()
}

func (s *Service) Entitlements(ctx context.Context, orgID uint, now time.Time) (*Entitlements, error) {
	s.mu.Lock()
	if e, ok := s.cache[orgID]; ok && s.clock().Before(e.expires) {
		s.mu.Unlock()
		ent := e.ent
		return &ent, nil
	}
	s.mu.Unlock()

	ent, err := s.resolve(ctx, orgID, now)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[orgID] = cacheEntry{ent: *ent, expires: s.clock().Add(EntitlementsTTL)}
	s.mu.Unlock()
	return ent, nil
}

// resolve reads the stored plan and overrides and applies Resolve.
func (s *Service) resolve(ctx context.Context, orgID uint, now time.Time) (*Entitlements, error) {
	plan, err := s.repo.GetOrgPlan(ctx, orgID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.repo.ListFeatureOverrides(ctx, orgID)
	if err != nil {
		return nil, err
	}
	ent := Resolve(*plan, overrides, now)
	return &ent, nil
}

func (s *Service) Summary(ctx context.Context, orgID uint, now time.Time) (*Summary, error) {
	ent, err := s.Entitlements(ctx, orgID, now)
	if err != nil {
		return nil, err
	}
	usage, err := s.repo.ActiveUsage(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &Summary{Entitlements: *ent, Usage: usage}, nil
}

func (s *Service) UsageByOrg(ctx context.Context) (map[uint]Usage, error) {
	return s.repo.ActiveUsageByOrg(ctx)
}

func (s *Service) EnforceLimit(tx *gorm.DB, orgID uint, kind LimitKind, now time.Time) error {
	plan, err := s.repo.LockOrgPlan(tx, orgID)
	if err != nil {
		return err
	}
	limits := Resolve(*plan, nil, now).Limits
	var max *int
	var current int
	switch kind {
	case LimitBranches:
		max = limits.Branches
		if max == nil {
			return nil
		}
		current, err = s.repo.CountActiveBranches(tx, orgID)
	case LimitDevices:
		max = limits.Devices
		if max == nil {
			return nil
		}
		current, err = s.repo.CountActiveDevices(tx, orgID)
	default:
		return common.SystemError("unknown plan limit")
	}
	if err != nil {
		return err
	}
	if current >= *max {
		return LimitReachedError(kind, *max, current)
	}
	return nil
}

func (s *Service) RecordChange(tx *gorm.DB, c *OrgPlanChange) error {
	return s.repo.CreatePlanChange(tx, c)
}

func (s *Service) ListPlans() PlanCatalog {
	return PlanCatalog{Plans: Plans(), Features: AllFeatures()}
}

func (s *Service) GetSubscription(ctx context.Context, orgID uint) (*Detail, error) {
	now := s.clock()
	plan, err := s.repo.GetOrgPlan(ctx, orgID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.repo.ListFeatureOverrides(ctx, orgID)
	if err != nil {
		return nil, err
	}
	usage, err := s.repo.ActiveUsage(ctx, orgID)
	if err != nil {
		return nil, err
	}
	history, err := s.repo.ListPlanChanges(ctx, orgID, HistoryLimit)
	if err != nil {
		return nil, err
	}
	ent := Resolve(*plan, overrides, now)
	if overrides == nil {
		overrides = []OrgFeatureOverride{}
	}
	if history == nil {
		history = []OrgPlanChange{}
	}
	return &Detail{
		OrganizationID: orgID, Effective: Summary{Entitlements: ent, Usage: usage}, Raw: *plan,
		OverLimit: ent.Limits.Exceeded(usage), Overrides: overrides, History: history,
	}, nil
}

// validateWho checks the two free-text fields every write carries.
func validateWho(changedBy, note string) (string, string, error) {
	changedBy, note = strings.TrimSpace(changedBy), strings.TrimSpace(note)
	if changedBy == "" {
		return "", "", common.BadRequestError("changed_by (the shagan_team admin's email) is required")
	}
	if len(changedBy) > 255 {
		return "", "", common.BadRequestError("changed_by must be at most 255 characters")
	}
	if len([]rune(note)) > 500 {
		return "", "", common.BadRequestError("note must be at most 500 characters")
	}
	return changedBy, note, nil
}

func samePlan(a, b OrgPlan) bool {
	if a.Plan != b.Plan || a.Trial != b.Trial || a.ExtraBranches != b.ExtraBranches || a.ExtraDevices != b.ExtraDevices {
		return false
	}
	if (a.ExpiresAt == nil) != (b.ExpiresAt == nil) {
		return false
	}
	return a.ExpiresAt == nil || a.ExpiresAt.Equal(*b.ExpiresAt)
}

func planAudit(p OrgPlan, extra map[string]any) map[string]any {
	m := map[string]any{"plan": p.Plan, "expires_at": p.ExpiresAt, "trial": p.Trial, "extra_branches": p.ExtraBranches, "extra_devices": p.ExtraDevices}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (s *Service) UpdateSubscription(ctx context.Context, orgID uint, in UpdateSubscriptionRequest) (*Detail, error) {
	changedBy, note, err := validateWho(in.ChangedBy, in.Note)
	if err != nil {
		return nil, err
	}
	if in.Plan != nil {
		if _, ok := LookupPlan(*in.Plan); !ok {
			return nil, common.BadRequestError("plan must be one of basic, pro, enterprise")
		}
	}
	if (in.ExtraBranches != nil && *in.ExtraBranches < 0) || (in.ExtraDevices != nil && *in.ExtraDevices < 0) {
		return nil, common.BadRequestError("extra_branches and extra_devices must be zero or greater")
	}

	changed := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		before, err := s.repo.LockOrgPlan(tx, orgID)
		if err != nil {
			return err
		}
		after := *before
		if in.Plan != nil {
			after.Plan = *in.Plan
		}
		if in.ExpiresAt.Set {
			after.ExpiresAt = in.ExpiresAt.Time
		}
		if in.Trial != nil {
			after.Trial = *in.Trial
		}
		if in.ExtraBranches != nil {
			after.ExtraBranches = *in.ExtraBranches
		}
		if in.ExtraDevices != nil {
			after.ExtraDevices = *in.ExtraDevices
		}
		if samePlan(*before, after) {
			return nil
		}
		changed = true
		if err := s.repo.UpdateOrgPlan(tx, orgID, after); err != nil {
			return err
		}
		if err := s.repo.CreatePlanChange(tx, &OrgPlanChange{
			OrgID: orgID, FromPlan: before.Plan, ToPlan: after.Plan, ExpiresAt: after.ExpiresAt, Trial: after.Trial,
			ExtraBranches: after.ExtraBranches, ExtraDevices: after.ExtraDevices, Note: note, ChangedBy: changedBy,
		}); err != nil {
			return err
		}
		return s.audit.CreateAuditLog(tx, &audit.AuditLog{
			OrgID: orgID, Entity: "organization_subscription", EntityID: strconv.FormatUint(uint64(orgID), 10), Action: "subscription_updated",
			Before: audit.ToJSON(planAudit(*before, nil)),
			After:  audit.ToJSON(planAudit(after, map[string]any{"changed_by": changedBy, "note": note})),
		})
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.invalidate(orgID)
	}
	return s.GetSubscription(ctx, orgID)
}

func (s *Service) SetFeatureOverride(ctx context.Context, orgID uint, feature string, in FeatureOverrideRequest) (*Detail, error) {
	changedBy, note, err := validateWho(in.ChangedBy, in.Note)
	if err != nil {
		return nil, err
	}
	if !IsFeature(feature) {
		return nil, common.BadRequestError("unknown feature " + strconv.Quote(feature))
	}
	if in.Enabled == nil {
		return nil, common.BadRequestError("enabled is required")
	}
	enabled := *in.Enabled

	err = s.db.Transaction(func(tx *gorm.DB) error {
		plan, err := s.repo.LockOrgPlan(tx, orgID)
		if err != nil {
			return err
		}
		previous, err := s.repo.ListFeatureOverrides(ctx, orgID)
		if err != nil {
			return err
		}
		var before any
		for _, o := range previous {
			if o.Feature == feature {
				before = map[string]any{"feature": feature, "enabled": o.Enabled}
			}
		}
		if err := s.repo.UpsertFeatureOverride(tx, &OrgFeatureOverride{OrgID: orgID, Feature: feature, Enabled: enabled}); err != nil {
			return err
		}
		state := "off"
		if enabled {
			state = "on"
		}
		return s.recordFeatureChange(tx, orgID, plan, changedBy, note, fmt.Sprintf("feature %s override: %s", feature, state),
			"feature_override_set", before, map[string]any{"feature": feature, "enabled": enabled, "changed_by": changedBy, "note": note})
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(orgID)
	return s.GetSubscription(ctx, orgID)
}

func (s *Service) ClearFeatureOverride(ctx context.Context, orgID uint, feature string, in FeatureOverrideRequest) (*Detail, error) {
	changedBy, note, err := validateWho(in.ChangedBy, in.Note)
	if err != nil {
		return nil, err
	}
	if !IsFeature(feature) {
		return nil, common.BadRequestError("unknown feature " + strconv.Quote(feature))
	}
	changed := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		plan, err := s.repo.LockOrgPlan(tx, orgID)
		if err != nil {
			return err
		}
		previous, err := s.repo.ListFeatureOverrides(ctx, orgID)
		if err != nil {
			return err
		}
		var before any
		for _, o := range previous {
			if o.Feature == feature {
				before = map[string]any{"feature": feature, "enabled": o.Enabled}
			}
		}
		removed, err := s.repo.DeleteFeatureOverride(tx, orgID, feature)
		if err != nil || !removed {
			return err
		}
		changed = true
		return s.recordFeatureChange(tx, orgID, plan, changedBy, note, fmt.Sprintf("feature %s override cleared (back to the plan's default)", feature),
			"feature_override_cleared", before, map[string]any{"feature": feature, "enabled": nil, "changed_by": changedBy, "note": note})
	})
	if err != nil {
		return nil, err
	}
	if changed {
		s.invalidate(orgID)
	}
	return s.GetSubscription(ctx, orgID)
}

// recordFeatureChange writes the history row and audit entry for a feature
// override change: the plan fields repeat the unchanged stored values and the
// note says what happened.
func (s *Service) recordFeatureChange(tx *gorm.DB, orgID uint, plan *OrgPlan, changedBy, userNote, what, action string, before, after any) error {
	note := what
	if userNote != "" {
		note += " - " + userNote
	}
	if len([]rune(note)) > 500 {
		note = string([]rune(note)[:500])
	}
	if err := s.repo.CreatePlanChange(tx, &OrgPlanChange{
		OrgID: orgID, FromPlan: plan.Plan, ToPlan: plan.Plan, ExpiresAt: plan.ExpiresAt, Trial: plan.Trial,
		ExtraBranches: plan.ExtraBranches, ExtraDevices: plan.ExtraDevices, Note: note, ChangedBy: changedBy,
	}); err != nil {
		return err
	}
	return s.audit.CreateAuditLog(tx, &audit.AuditLog{
		OrgID: orgID, Entity: "organization_subscription", EntityID: strconv.FormatUint(uint64(orgID), 10), Action: action,
		Before: audit.ToJSON(before), After: audit.ToJSON(after),
	})
}
