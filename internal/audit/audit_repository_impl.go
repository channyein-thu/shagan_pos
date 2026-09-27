package audit

import (
	"context"

	"gorm.io/gorm"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

func (r *RepositoryImpl) CreateAuditLog(db *gorm.DB, entry *AuditLog) error {
	return db.Create(entry).Error
}

func (r *RepositoryImpl) ListAuditLog(ctx context.Context, orgID uint, branchID *uint) ([]AuditLog, error) {
	var entries []AuditLog
	q := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if err := q.Order("created_at DESC, id DESC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}
