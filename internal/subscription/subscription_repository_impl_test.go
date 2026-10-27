//go:build cgo

package subscription_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/subscription"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&identity.Organization{}, &identity.Branch{}, &identity.Device{}, &common.DataMigration{}))
	return db
}

func addBranch(t *testing.T, db *gorm.DB, orgID uint, status identity.BranchStatus) *identity.Branch {
	t.Helper()
	b := &identity.Branch{OrgID: orgID, Name: "B", Status: status, Address: "-", Phone: "-"}
	require.NoError(t, db.Create(b).Error)
	return b
}

func addDevice(t *testing.T, db *gorm.DB, branchID uint, status identity.DeviceStatus) {
	t.Helper()
	require.NoError(t, db.Create(&identity.Device{BranchID: branchID, Name: "D", Status: status, LastSeenAt: time.Now()}).Error)
}

// The plan columns arrive with their defaults: every organization that existed
// before is Basic, no trial, no end date, no extras.
func TestOrganizationPlanColumns_DefaultToBasicWithNoTrial(t *testing.T) {
	db := newDB(t)
	require.NoError(t, db.Exec(`INSERT INTO organizations (name, status, timezone, created_at, updated_at) VALUES ('Old Org', 'active', 'Asia/Yangon', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)

	var org identity.Organization
	require.NoError(t, db.First(&org).Error)

	require.Equal(t, "basic", org.Plan)
	require.Nil(t, org.PlanExpiresAt)
	require.False(t, org.PlanTrial)
	require.Zero(t, org.ExtraBranches)
	require.Zero(t, org.ExtraDevices)
}

// The named data migration gives every existing org one history row, once.
func TestAutoMigrate_StartsExistingOrgsOnBasicWithOneHistoryRow_Once(t *testing.T) {
	db := newDB(t)
	require.NoError(t, db.Exec(`INSERT INTO organizations (name, status, timezone, created_at, updated_at) VALUES ('A', 'active', 'Asia/Yangon', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), ('B', 'active', 'Asia/Yangon', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)

	require.NoError(t, subscription.AutoMigrate(db))
	require.NoError(t, subscription.AutoMigrate(db)) // a second start does nothing

	var changes []subscription.OrgPlanChange
	require.NoError(t, db.Order("org_id").Find(&changes).Error)
	require.Len(t, changes, 2)
	for _, c := range changes {
		require.Equal(t, "basic", c.ToPlan)
		require.False(t, c.Trial)
		require.Equal(t, "migration", c.ChangedBy)
	}

	// an org created AFTER the migration is not touched by a later start
	require.NoError(t, db.Exec(`INSERT INTO organizations (name, status, timezone, plan, plan_trial, created_at, updated_at) VALUES ('New', 'active', 'Asia/Yangon', 'pro', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error)
	require.NoError(t, subscription.AutoMigrate(db))
	var total int64
	require.NoError(t, db.Model(&subscription.OrgPlanChange{}).Count(&total).Error)
	require.Equal(t, int64(2), total)
	var newOrg identity.Organization
	require.NoError(t, db.Where("name = ?", "New").First(&newOrg).Error)
	require.Equal(t, "pro", newOrg.Plan)
	require.True(t, newOrg.PlanTrial)
}

func TestRepository_CountsOnlyActiveRows_PerOrg(t *testing.T) {
	db := newDB(t)
	require.NoError(t, subscription.AutoMigrate(db))
	repo := subscription.NewRepository(db)
	// org 1: two active branches, one inactive; devices: active x2 on the active
	// branches, inactive x1 and revoked x1 (the suspended ones don't count)
	b1, b2 := addBranch(t, db, 1, identity.BranchStatusActive), addBranch(t, db, 1, identity.BranchStatusActive)
	b3 := addBranch(t, db, 1, identity.BranchStatusInactive)
	addDevice(t, db, b1.ID, identity.DeviceStatusActive)
	addDevice(t, db, b2.ID, identity.DeviceStatusActive)
	addDevice(t, db, b2.ID, identity.DeviceStatusInactive)
	addDevice(t, db, b3.ID, identity.DeviceStatusRevoked)
	// org 2 must not leak into org 1's counts
	o2 := addBranch(t, db, 2, identity.BranchStatusActive)
	addDevice(t, db, o2.ID, identity.DeviceStatusActive)

	branches, err := repo.CountActiveBranches(db, 1)
	require.NoError(t, err)
	devices, err := repo.CountActiveDevices(db, 1)
	require.NoError(t, err)
	usage, err := repo.ActiveUsage(context.Background(), 1)
	require.NoError(t, err)
	all, err := repo.ActiveUsageByOrg(context.Background())
	require.NoError(t, err)

	require.Equal(t, 2, branches)
	require.Equal(t, 2, devices)
	require.Equal(t, subscription.Usage{Branches: 2, Devices: 2}, usage)
	require.Equal(t, subscription.Usage{Branches: 2, Devices: 2}, all[1])
	require.Equal(t, subscription.Usage{Branches: 1, Devices: 1}, all[2])
}

func TestRepository_OrgPlanRoundTrip_AndUnknownOrg(t *testing.T) {
	db := newDB(t)
	require.NoError(t, subscription.AutoMigrate(db))
	repo := subscription.NewRepository(db)
	org := &identity.Organization{Name: "Acme", Timezone: "Asia/Yangon", Plan: "basic"}
	require.NoError(t, db.Create(org).Error)
	end := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, repo.UpdateOrgPlan(db, org.ID, subscription.OrgPlan{Plan: "pro", ExpiresAt: &end, Trial: true, ExtraBranches: 2, ExtraDevices: 3}))
	got, err := repo.GetOrgPlan(context.Background(), org.ID)

	require.NoError(t, err)
	require.Equal(t, "pro", got.Plan)
	require.True(t, end.Equal(*got.ExpiresAt))
	require.True(t, got.Trial)
	require.Equal(t, 2, got.ExtraBranches)
	require.Equal(t, 3, got.ExtraDevices)

	// clearing the end date really writes NULL
	require.NoError(t, repo.UpdateOrgPlan(db, org.ID, subscription.OrgPlan{Plan: "pro"}))
	got, err = repo.GetOrgPlan(context.Background(), org.ID)
	require.NoError(t, err)
	require.Nil(t, got.ExpiresAt)

	_, err = repo.GetOrgPlan(context.Background(), 9999)
	var restErr common.RestError
	require.ErrorAs(t, err, &restErr)
	require.Equal(t, http.StatusNotFound, restErr.Status)
}

func TestRepository_FeatureOverrideUpsertsAndDeletes_AndHistoryIsNewestFirst(t *testing.T) {
	db := newDB(t)
	require.NoError(t, subscription.AutoMigrate(db))
	repo := subscription.NewRepository(db)

	require.NoError(t, repo.UpsertFeatureOverride(db, &subscription.OrgFeatureOverride{OrgID: 1, Feature: "loyalty", Enabled: true}))
	require.NoError(t, repo.UpsertFeatureOverride(db, &subscription.OrgFeatureOverride{OrgID: 1, Feature: "loyalty", Enabled: false}))
	require.NoError(t, repo.UpsertFeatureOverride(db, &subscription.OrgFeatureOverride{OrgID: 2, Feature: "loyalty", Enabled: true}))
	got, err := repo.ListFeatureOverrides(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, got, 1, "one row per (org, feature)")
	require.False(t, got[0].Enabled)

	removed, err := repo.DeleteFeatureOverride(db, 1, "loyalty")
	require.NoError(t, err)
	require.True(t, removed)
	removed, err = repo.DeleteFeatureOverride(db, 1, "loyalty")
	require.NoError(t, err)
	require.False(t, removed)

	for i := 1; i <= 3; i++ {
		require.NoError(t, repo.CreatePlanChange(db, &subscription.OrgPlanChange{OrgID: 1, ToPlan: "pro", Note: fmt.Sprint(i), ChangedBy: "a@shagan.test"}))
	}
	history, err := repo.ListPlanChanges(context.Background(), 1, 2)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "3", history[0].Note)
	require.Equal(t, "2", history[1].Note)
}
