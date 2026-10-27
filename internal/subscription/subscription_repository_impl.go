package subscription

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// planRow is the plan block as it sits on the organizations table.
type planRow struct {
	Plan          string
	PlanExpiresAt *time.Time
	PlanTrial     bool
	ExtraBranches int
	ExtraDevices  int
}

func (r planRow) orgPlan() *OrgPlan {
	return &OrgPlan{Plan: r.Plan, ExpiresAt: r.PlanExpiresAt, Trial: r.PlanTrial, ExtraBranches: r.ExtraBranches, ExtraDevices: r.ExtraDevices}
}

const planColumns = "plan, plan_expires_at, plan_trial, extra_branches, extra_devices"

func (r *RepositoryImpl) readPlan(q *gorm.DB, orgID uint) (*OrgPlan, error) {
	var rows []planRow
	if err := q.Table("organizations").Select(planColumns).Where("id = ?", orgID).Limit(1).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, common.NotFoundError("organization not found")
	}
	return rows[0].orgPlan(), nil
}

func (r *RepositoryImpl) GetOrgPlan(ctx context.Context, orgID uint) (*OrgPlan, error) {
	return r.readPlan(r.db.WithContext(ctx), orgID)
}

func (r *RepositoryImpl) LockOrgPlan(db *gorm.DB, orgID uint) (*OrgPlan, error) {
	return r.readPlan(db.Clauses(clause.Locking{Strength: "UPDATE"}), orgID)
}

func (r *RepositoryImpl) UpdateOrgPlan(db *gorm.DB, orgID uint, p OrgPlan) error {
	return db.Table("organizations").Where("id = ?", orgID).Updates(map[string]any{
		"plan": p.Plan, "plan_expires_at": p.ExpiresAt, "plan_trial": p.Trial,
		"extra_branches": p.ExtraBranches, "extra_devices": p.ExtraDevices,
	}).Error
}

func (r *RepositoryImpl) CountActiveBranches(db *gorm.DB, orgID uint) (int, error) {
	var n int64
	err := db.Table("branches").Where("org_id = ? AND status = ?", orgID, "active").Count(&n).Error
	return int(n), err
}

func (r *RepositoryImpl) CountActiveDevices(db *gorm.DB, orgID uint) (int, error) {
	var n int64
	err := db.Table("devices").
		Where("status = ? AND branch_id IN (?)", "active", db.Session(&gorm.Session{NewDB: true}).Table("branches").Select("id").Where("org_id = ?", orgID)).
		Count(&n).Error
	return int(n), err
}

func (r *RepositoryImpl) ActiveUsage(ctx context.Context, orgID uint) (Usage, error) {
	db := r.db.WithContext(ctx)
	branches, err := r.CountActiveBranches(db, orgID)
	if err != nil {
		return Usage{}, err
	}
	devices, err := r.CountActiveDevices(db.Session(&gorm.Session{NewDB: true}), orgID)
	if err != nil {
		return Usage{}, err
	}
	return Usage{Branches: branches, Devices: devices}, nil
}

func (r *RepositoryImpl) ActiveUsageByOrg(ctx context.Context) (map[uint]Usage, error) {
	usage := map[uint]Usage{}
	var branches []struct {
		OrgID uint
		N     int
	}
	if err := r.db.WithContext(ctx).Table("branches").Select("org_id, COUNT(*) AS n").
		Where("status = ?", "active").Group("org_id").Scan(&branches).Error; err != nil {
		return nil, err
	}
	for _, b := range branches {
		u := usage[b.OrgID]
		u.Branches = b.N
		usage[b.OrgID] = u
	}
	var devices []struct {
		OrgID uint
		N     int
	}
	if err := r.db.WithContext(ctx).Table("devices").
		Select("branches.org_id AS org_id, COUNT(*) AS n").
		Joins("JOIN branches ON branches.id = devices.branch_id").
		Where("devices.status = ?", "active").Group("branches.org_id").Scan(&devices).Error; err != nil {
		return nil, err
	}
	for _, d := range devices {
		u := usage[d.OrgID]
		u.Devices = d.N
		usage[d.OrgID] = u
	}
	return usage, nil
}

func (r *RepositoryImpl) ListFeatureOverrides(ctx context.Context, orgID uint) ([]OrgFeatureOverride, error) {
	var rows []OrgFeatureOverride
	err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("feature").Find(&rows).Error
	return rows, err
}

func (r *RepositoryImpl) UpsertFeatureOverride(db *gorm.DB, o *OrgFeatureOverride) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_id"}, {Name: "feature"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "updated_at"}),
	}).Create(o).Error
}

func (r *RepositoryImpl) DeleteFeatureOverride(db *gorm.DB, orgID uint, feature string) (bool, error) {
	res := db.Where("org_id = ? AND feature = ?", orgID, feature).Delete(&OrgFeatureOverride{})
	return res.RowsAffected > 0, res.Error
}

func (r *RepositoryImpl) CreatePlanChange(db *gorm.DB, c *OrgPlanChange) error {
	return db.Create(c).Error
}

func (r *RepositoryImpl) ListPlanChanges(ctx context.Context, orgID uint, limit int) ([]OrgPlanChange, error) {
	var rows []OrgPlanChange
	err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
