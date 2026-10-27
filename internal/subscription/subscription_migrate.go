package subscription

import (
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

const migrationOrganizationPlans = "2026-10-27-organization-plans"

// AutoMigrate creates the subscription tables, then runs the one-time data
// migration. It must run after identity's AutoMigrate, which adds the plan
// columns (default 'basic', no trial, no extras) to organizations - so every
// organization that existed before is already Basic with no trial.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(&OrgFeatureOverride{}, &OrgPlanChange{}); err != nil {
		return err
	}
	return common.RunOnce(db, migrationOrganizationPlans, startExistingOrgsOnBasic)
}

// startExistingOrgsOnBasic is the named data migration for subscription tiers:
// every organization that exists now is made Basic with no trial and no end date
// (a no-op for rows the column default already covered, and it normalises any
// blank value), and gets one history row so shagan_team shows where its plan
// started. New organizations are not touched - they start on a Pro trial.
func startExistingOrgsOnBasic(tx *gorm.DB) error {
	if err := tx.Exec(`UPDATE organizations SET plan = ?, plan_trial = ?, plan_expires_at = NULL
		WHERE plan IS NULL OR plan = ''`, PlanBasic, false).Error; err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO org_plan_changes
			(org_id, from_plan, to_plan, expires_at, trial, extra_branches, extra_devices, note, changed_by, created_at)
		SELECT o.id, '', o.plan, o.plan_expires_at, o.plan_trial, o.extra_branches, o.extra_devices,
			'existing organization set to its starting plan', 'migration', CURRENT_TIMESTAMP
		FROM organizations o
		WHERE NOT EXISTS (SELECT 1 FROM org_plan_changes c WHERE c.org_id = o.id)`).Error
}
