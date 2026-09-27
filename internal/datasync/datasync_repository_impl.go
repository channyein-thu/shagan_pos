package datasync

import (
	"context"
	"errors"
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

func (r *RepositoryImpl) UnresolvedConflictCount(ctx context.Context, branchIDs []uint) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("sync_conflicts").
		Joins("JOIN sales ON sales.id = sync_conflicts.sale_id").
		Where("sales.branch_id IN (?) AND sync_conflicts.resolved_at IS NULL", branchIDs).
		Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *RepositoryImpl) LastSyncedAt(ctx context.Context, branchIDs []uint) (*time.Time, error) {
	var lastSyncedAt *time.Time
	err := r.db.WithContext(ctx).
		Table("sales").
		Select("MAX(synced_at)").
		Where("branch_id IN (?) AND synced_at IS NOT NULL", branchIDs).
		Scan(&lastSyncedAt).Error
	if err != nil {
		return nil, err
	}
	return lastSyncedAt, nil
}

func (r *RepositoryImpl) ListSyncConflicts(ctx context.Context, branchIDs []uint) ([]SyncConflict, error) {
	var conflicts []SyncConflict
	err := r.db.WithContext(ctx).
		Table("sync_conflicts").
		Select("sync_conflicts.*").
		Joins("JOIN sales ON sales.id = sync_conflicts.sale_id").
		Where("sales.branch_id IN (?)", branchIDs).
		Order("sync_conflicts.id DESC").
		Find(&conflicts).Error
	if err != nil {
		return nil, err
	}
	return conflicts, nil
}

func (r *RepositoryImpl) GetSyncConflictWithLock(db *gorm.DB, branchIDs []uint, id uint) (*SyncConflict, error) {
	var conflict SyncConflict
	err := db.Table("sync_conflicts").
		Select("sync_conflicts.*").
		Joins("JOIN sales ON sales.id = sync_conflicts.sale_id").
		Where("sync_conflicts.id = ? AND sales.branch_id IN (?)", id, branchIDs).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&conflict).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("sync conflict not found")
		}
		return nil, err
	}
	return &conflict, nil
}

func (r *RepositoryImpl) UpdateSyncConflictResolved(db *gorm.DB, id uint, resolvedBy uint, resolvedAt time.Time) error {
	return db.Model(&SyncConflict{}).Where("id = ?", id).
		Updates(map[string]any{"resolved_by": resolvedBy, "resolved_at": resolvedAt}).Error
}

func (r *RepositoryImpl) CreateSyncConflict(ctx context.Context, c *SyncConflict) error {
	return r.db.WithContext(ctx).Create(c).Error
}
