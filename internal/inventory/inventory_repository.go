package inventory

import "context"

// Repository defines the inventory domain's persistence operations.
type Repository interface {
	// ListStockLevels is a plain query - branchIDs is already resolved by
	// the service (the caller's own org's branches, or one specific
	// verified branch), and productID additionally restricts to one
	// product when set. No business decision about which branches the
	// caller is allowed to see happens here.
	ListStockLevels(ctx context.Context, branchIDs []uint, productID *uint) ([]StockLevel, error)
	ListLowStock(ctx context.Context) ([]StockLevel, error)
	ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error)
	CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error)
	ListStockTransfers(ctx context.Context) ([]StockTransfer, error)
	CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error)
	UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error)
}
