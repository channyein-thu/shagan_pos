package inventory

import "context"

// Repository defines the inventory domain's persistence operations.
type Repository interface {
	ListStockLevels(ctx context.Context) ([]StockLevel, error)
	ListLowStock(ctx context.Context) ([]StockLevel, error)
	ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error)
	CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error)
	ListStockTransfers(ctx context.Context) ([]StockTransfer, error)
	CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error)
	UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error)
}
