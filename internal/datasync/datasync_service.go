package datasync

import "context"

// Interface defines the datasync domain's use cases.
type Interface interface {
	GetCatalogSnapshot(ctx context.Context) (map[string]any, error)
	IngestQueuedSales(ctx context.Context) (map[string]any, error)
	FlushSync(ctx context.Context) (map[string]any, error)
	GetSyncStatus(ctx context.Context) (map[string]any, error)
	ListSyncConflicts(ctx context.Context) ([]SyncConflict, error)
	ResolveSyncConflict(ctx context.Context, id uint) (*SyncConflict, error)
}
