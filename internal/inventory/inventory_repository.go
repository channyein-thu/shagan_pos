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
	// ListInventoryLedger backs `GET /inventory/ledger`, scoped to orgID -
	// InventoryLedger carries its own OrgID directly (unlike StockLevel),
	// so branchID/productID can be applied as plain AND conditions with no
	// separate ownership check needed: a row can only match org_id=orgID
	// AND branch_id=someone-else's-branch if such a row genuinely exists,
	// which it can't. Ordered oldest-first, since each row's BalanceAfter
	// only reads as a running balance in chronological order.
	ListInventoryLedger(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]InventoryLedger, error)
	CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error)
	ListStockTransfers(ctx context.Context) ([]StockTransfer, error)
	CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error)
	UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error)
}
