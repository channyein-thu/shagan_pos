package inventory

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListStockLevels(ctx context.Context) ([]StockLevel, error) {
	return s.repo.ListStockLevels(ctx)
}

func (s *Service) ListLowStock(ctx context.Context) ([]StockLevel, error) {
	return s.repo.ListLowStock(ctx)
}

func (s *Service) ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error) {
	return s.repo.ListInventoryLedger(ctx)
}

func (s *Service) CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error) {
	return s.repo.CreateStockAdjustment(ctx, in)
}

func (s *Service) ListStockTransfers(ctx context.Context) ([]StockTransfer, error) {
	return s.repo.ListStockTransfers(ctx)
}

func (s *Service) CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error) {
	return s.repo.CreateStockTransfer(ctx, in)
}

func (s *Service) UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error) {
	return s.repo.UpdateStockTransfer(ctx, id, in)
}
