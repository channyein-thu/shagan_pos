package audit

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// ListAuditLog backs `GET /audit-log`. Read-only; written via mutation hooks, not a public POST
func (r *RepositoryImpl) ListAuditLog(ctx context.Context) ([]AuditLog, error) {
	return nil, common.ErrNotImplemented
}
