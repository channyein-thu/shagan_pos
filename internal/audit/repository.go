package audit

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListAuditLog backs `GET /audit-log`. Read-only; written via mutation hooks, not a public POST
func (r *Repository) ListAuditLog(ctx context.Context) ([]AuditLog, error) {
	return nil, common.ErrNotImplemented
}
