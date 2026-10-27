package subscription

import (
	"context"
	"time"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
)

// AuditWriter is what subscription needs from audit: every subscription write
// is recorded with before / after. audit.Repository already satisfies this
// signature - no adapter needed.
type AuditWriter interface {
	CreateAuditLog(db *gorm.DB, entry *audit.AuditLog) error
}

// Interface defines the subscription domain's use cases.
type Interface interface {
	// Entitlements is the resolver (see Resolve) for one org, cached for a
	// short time per org (EntitlementsTTL) and cleared by every write below.
	// It is what RequireFeature, /me and /sync/catalog read - never the JWT:
	// a staff token lives 12 h and a device's refresh token 30 days, so a plan
	// in a token would be wrong for that long.
	Entitlements(ctx context.Context, orgID uint, now time.Time) (*Entitlements, error)
	// Summary is Entitlements plus live usage (active branches / devices) -
	// GET /me. Usage is never cached.
	Summary(ctx context.Context, orgID uint, now time.Time) (*Summary, error)
	// UsageByOrg is every org's active usage, for the organization list's
	// over_limit flag.
	UsageByOrg(ctx context.Context) (map[uint]Usage, error)
	// EnforceLimit is the check behind creating or reactivating a branch or
	// device. tx must be the transaction that then makes the insert / status
	// change: it locks the organization row (so two parallel creates are
	// serialized), reads the stored plan fresh (never the cache), counts the
	// ACTIVE rows and refuses with a 403 plan_limit_reached when one more would
	// exceed the limit. An unlimited limit passes.
	EnforceLimit(tx *gorm.DB, orgID uint, kind LimitKind, now time.Time) error
	// RecordChange appends a history row inside tx (a new organization's trial).
	RecordChange(tx *gorm.DB, c *OrgPlanChange) error

	// ListPlans backs `GET /internal/plans`.
	ListPlans() PlanCatalog
	// GetSubscription backs `GET /internal/organizations/:id/subscription`
	// (404 for an unknown org).
	GetSubscription(ctx context.Context, orgID uint) (*Detail, error)
	// UpdateSubscription backs `PATCH .../subscription`: validates (400 for an
	// unknown plan, a negative extra, a missing changed_by), writes the plan
	// block, a history row and an audit entry in one transaction, and clears the
	// cache. A request that changes nothing writes nothing. Nothing is ever
	// deleted or switched off by a downgrade - an org over its new limits keeps
	// what it has and can't add more.
	UpdateSubscription(ctx context.Context, orgID uint, in UpdateSubscriptionRequest) (*Detail, error)
	// SetFeatureOverride / ClearFeatureOverride back `PUT / DELETE
	// .../features/:code`. The code must be a known feature (400). Each real
	// change writes a history row (the plan fields unchanged, Note saying what
	// happened) and an audit entry; clearing an override that isn't there is a
	// no-op.
	SetFeatureOverride(ctx context.Context, orgID uint, feature string, in FeatureOverrideRequest) (*Detail, error)
	ClearFeatureOverride(ctx context.Context, orgID uint, feature string, in FeatureOverrideRequest) (*Detail, error)
}
