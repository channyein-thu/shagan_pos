package subscription

import (
	"context"

	"gorm.io/gorm"
)

// Repository is the subscription domain's persistence. The organization's own
// plan columns live on identity's organizations table; this reads and writes
// just that block (by table name, so subscription never imports identity), plus
// its own two tables and the active branch / device counts.
type Repository interface {
	// GetOrgPlan reads an org's stored plan block. common.NotFoundError for an
	// unknown org.
	GetOrgPlan(ctx context.Context, orgID uint) (*OrgPlan, error)
	// LockOrgPlan is GetOrgPlan inside a transaction that takes a row lock on
	// the organization (SELECT ... FOR UPDATE): everything that adds or
	// reactivates a branch or device takes it first, so two parallel creates
	// are serialized and can't both pass the same count.
	LockOrgPlan(db *gorm.DB, orgID uint) (*OrgPlan, error)
	// UpdateOrgPlan writes the whole plan block.
	UpdateOrgPlan(db *gorm.DB, orgID uint, p OrgPlan) error
	// CountActiveBranches / CountActiveDevices count the org's rows whose
	// status is "active". Devices belong to an org through their branch.
	CountActiveBranches(db *gorm.DB, orgID uint) (int, error)
	CountActiveDevices(db *gorm.DB, orgID uint) (int, error)
	// ActiveUsage is both counts for one org, outside any transaction - GET /me
	// and the internal detail.
	ActiveUsage(ctx context.Context, orgID uint) (Usage, error)
	// ActiveUsageByOrg is both counts for every org that has any, in one pass -
	// the organization list's over_limit flag.
	ActiveUsageByOrg(ctx context.Context) (map[uint]Usage, error)
	ListFeatureOverrides(ctx context.Context, orgID uint) ([]OrgFeatureOverride, error)
	// UpsertFeatureOverride inserts or, on the unique (org_id, feature),
	// replaces enabled.
	UpsertFeatureOverride(db *gorm.DB, o *OrgFeatureOverride) error
	// DeleteFeatureOverride removes the override; false if there was none.
	DeleteFeatureOverride(db *gorm.DB, orgID uint, feature string) (bool, error)
	// CreatePlanChange appends a history row.
	CreatePlanChange(db *gorm.DB, c *OrgPlanChange) error
	// ListPlanChanges returns the newest limit entries, newest first.
	ListPlanChanges(ctx context.Context, orgID uint, limit int) ([]OrgPlanChange, error)
}
