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
	// ListLowStock is the same shape as ListStockLevels, but joins to
	// catalog's products table to keep only rows where qty <= the
	// product's own threshold.
	ListLowStock(ctx context.Context, branchIDs []uint) ([]StockLevel, error)
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
	// CreateStockAdjustment backs `POST /inventory/adjustments`. Plain
	// insert - the service has already resolved BranchID/ActorID and
	// computed nothing here; the stock/ledger side effects are separate
	// primitives (GetStockLevel/CreateStockLevel/UpdateStockLevelQty/
	// CreateInventoryLedgerEntry above), composed by the service inside one
	// transaction, same shape as procurement.Service.CreateGoodsReceipt.
	CreateStockAdjustment(db *gorm.DB, adjustment *StockAdjustment) error
	// ListStockTransfers is a plain query, same branchIDs-already-resolved
	// reasoning as ListStockLevels - matches a transfer where branchIDs
	// contains either FromBranch or ToBranch when branchIDs is non-empty,
	// or lists every transfer when it's the full org (branchIDs still
	// passed, just covering every branch in the org).
	ListStockTransfers(ctx context.Context, branchIDs []uint) ([]StockTransfer, error)
	// GetStockTransfer backs UpdateStockTransfer's existence check -
	// returns common.NotFoundError if id doesn't exist or its FromBranch
	// isn't in branchIDs (the org's own branches) - not-found-not-forbidden,
	// same reasoning as everywhere else. lock requests a row lock (for the
	// completing-a-transfer path, guarding against two concurrent
	// completions of the same transfer) - db is always the in-flight
	// transaction for that path, so the lock actually participates in it,
	// same db-is-either-plain-or-in-flight-transaction reasoning as
	// GetStockLevel.
	GetStockTransfer(db *gorm.DB, branchIDs []uint, id uint, lock bool) (*StockTransfer, error)
	// ListStockTransferItemsByTransferIDs backs ListStockTransfers' line
	// items - one query across every transfer on the page, ordered by id so
	// a transfer's lines come back in the order they were created.
	ListStockTransferItemsByTransferIDs(ctx context.Context, transferIDs []uint) ([]StockTransferItem, error)
	// ListStockTransferItems backs the completing-a-transfer path - the
	// line items to actually move. Same db reasoning as GetStockTransfer.
	ListStockTransferItems(db *gorm.DB, transferID uint) ([]StockTransferItem, error)
	// CreateStockTransfer and CreateStockTransferItems are the plain
	// inserts Service.CreateStockTransfer composes inside one transaction -
	// a transfer without its line items should never exist, same reasoning
	// as procurement's PurchaseOrder/PurchaseOrderItems.
	CreateStockTransfer(db *gorm.DB, transfer *StockTransfer) error
	CreateStockTransferItems(db *gorm.DB, items []StockTransferItem) error
	// UpdateStockTransferStatus backs every status transition - plain
	// field write, the decision of which transitions are valid and what
	// else happens alongside one belongs to the service.
	UpdateStockTransferStatus(db *gorm.DB, id uint, status TransferStatus) error
}
