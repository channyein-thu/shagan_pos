package datasync

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

// GetCatalogSnapshot backs `GET /sync/catalog`. Snapshot + ETag for offline caching (products/categories/combos)
func (r *RepositoryImpl) GetCatalogSnapshot(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// IngestQueuedSales backs `POST /sync/sales`. Queued-sale ingest, idempotent
func (r *RepositoryImpl) IngestQueuedSales(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// FlushSync backs `POST /sync/flush`.
func (r *RepositoryImpl) FlushSync(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetSyncStatus backs `GET /sync/status`.
func (r *RepositoryImpl) GetSyncStatus(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ListSyncConflicts backs `GET /sync/conflicts`.
func (r *RepositoryImpl) ListSyncConflicts(ctx context.Context) ([]SyncConflict, error) {
	return nil, common.ErrNotImplemented
}

// ResolveSyncConflict backs `PATCH /sync/conflicts/:id/resolve`. Sets resolved_by/resolved_at
func (r *RepositoryImpl) ResolveSyncConflict(ctx context.Context, id uint) (*SyncConflict, error) {
	return nil, common.ErrNotImplemented
}
