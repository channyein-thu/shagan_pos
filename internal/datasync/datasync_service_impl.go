package datasync

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) GetCatalogSnapshot(ctx context.Context) (map[string]any, error) {
	return s.repo.GetCatalogSnapshot(ctx)
}

func (s *Service) IngestQueuedSales(ctx context.Context) (map[string]any, error) {
	return s.repo.IngestQueuedSales(ctx)
}

func (s *Service) FlushSync(ctx context.Context) (map[string]any, error) {
	return s.repo.FlushSync(ctx)
}

func (s *Service) GetSyncStatus(ctx context.Context) (map[string]any, error) {
	return s.repo.GetSyncStatus(ctx)
}

func (s *Service) ListSyncConflicts(ctx context.Context) ([]SyncConflict, error) {
	return s.repo.ListSyncConflicts(ctx)
}

func (s *Service) ResolveSyncConflict(ctx context.Context, id uint) (*SyncConflict, error) {
	return s.repo.ResolveSyncConflict(ctx, id)
}
