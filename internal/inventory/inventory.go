package inventory

import "context"

// Interface defines the inventory domain's use cases.
type Interface interface {
	ListStockLevels(ctx context.Context) ([]StockLevel, error)
	ListLowStock(ctx context.Context) ([]StockLevel, error)
	ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error)
	CreateStockAdjustment(ctx context.Context, in StockAdjustment) (*StockAdjustment, error)
	ListStockTransfers(ctx context.Context) ([]StockTransfer, error)
	CreateStockTransfer(ctx context.Context, in StockTransfer) (*StockTransfer, error)
	UpdateStockTransfer(ctx context.Context, id uint, in StockTransfer) (*StockTransfer, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
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

func (s *Service) CreateStockAdjustment(ctx context.Context, in StockAdjustment) (*StockAdjustment, error) {
	return s.repo.CreateStockAdjustment(ctx, in)
}

func (s *Service) ListStockTransfers(ctx context.Context) ([]StockTransfer, error) {
	return s.repo.ListStockTransfers(ctx)
}

func (s *Service) CreateStockTransfer(ctx context.Context, in StockTransfer) (*StockTransfer, error) {
	return s.repo.CreateStockTransfer(ctx, in)
}

func (s *Service) UpdateStockTransfer(ctx context.Context, id uint, in StockTransfer) (*StockTransfer, error) {
	return s.repo.UpdateStockTransfer(ctx, id, in)
}
