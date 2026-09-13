package datasync

import "context"

// Repository defines the datasync domain's persistence operations.
type Repository interface {
	GetCatalogSnapshot(ctx context.Context) (map[string]any, error)
	IngestQueuedSales(ctx context.Context) (map[string]any, error)
	FlushSync(ctx context.Context) (map[string]any, error)
	GetSyncStatus(ctx context.Context) (map[string]any, error)
	ListSyncConflicts(ctx context.Context) ([]SyncConflict, error)
	ResolveSyncConflict(ctx context.Context, id uint) (*SyncConflict, error)
}
