package inventory

import (
	"context"

	"gorm.io/gorm"
)

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
	// GetStockLevel backs procurement.Service.CreateGoodsReceipt's
	// find-or-create step (crediting a received product's stock at its
	// branch). Returns (nil, nil) when no row exists yet for this
	// product/branch pair - that's a legitimate state (a product with no
	// stock movements yet), not an error, so the caller decides whether to
	// create or update from a nil result rather than handling a
	// NotFoundError. db is either the repository's normal connection or an
	// in-flight transaction handed down by the caller.
	GetStockLevel(db *gorm.DB, productID uint, branchID uint) (*StockLevel, error)
	// CreateStockLevel backs the first-ever-stock-movement path for a
	// product/branch pair. Plain insert, same
	// db-is-either-plain-or-in-flight-transaction reasoning as
	// GetStockLevel above.
	CreateStockLevel(db *gorm.DB, level *StockLevel) error
	// UpdateStockLevelQty backs the already-exists path. Plain write - the
	// new value is already computed by the caller, same
	// db-is-either-plain-or-in-flight-transaction reasoning as
	// GetStockLevel above.
	UpdateStockLevelQty(db *gorm.DB, id uint, qty int) error
	// CreateInventoryLedgerEntry backs every audit-trail write in this
	// domain (stock adjustments, purchase receipts, transfers, ...). Plain
	// insert, same db-is-either-plain-or-in-flight-transaction reasoning as
	// GetStockLevel above.
	CreateInventoryLedgerEntry(db *gorm.DB, entry *InventoryLedger) error
	CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error)
	ListStockTransfers(ctx context.Context) ([]StockTransfer, error)
	CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error)
	UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error)
}
