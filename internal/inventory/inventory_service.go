package inventory

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/identity"
)

// BranchLookup is what inventory needs from identity: StockLevel carries no
// OrgID of its own (only BranchID) - Branch, not StockLevel, is what
// actually carries OrgID - so resolving "which branches belong to this
// org" (or confirming one specific branch does) is how org-scoping gets
// enforced here. identity.Repository already satisfies this signature - no
// adapter needed, same reasoning as catalog.BranchLookup.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
	ListBranches(ctx context.Context, orgID uint) ([]identity.Branch, error)
}

// ProductLookup is what inventory needs from catalog: confirming a
// client-supplied ProductID actually belongs to the caller's org (products
// are org-wide, see catalog.Product's doc - the branch a stock
// adjustment/transfer applies to is a separate, explicit client-supplied
// BranchID, verified via BranchLookup instead). Same reasoning as
// procurement.ProductLookup; catalog.Repository already satisfies this
// signature, no adapter needed.
type ProductLookup interface {
	GetProduct(ctx context.Context, orgID uint, id uint) (*catalog.Product, error)
	// UpdateProduct backs CreateStockAdjustment's optional weighted-average
	// cost blend (see CreateStockAdjustmentRequest.UnitCost's own doc) - db
	// is the same in-flight transaction the stock/ledger writes participate
	// in.
	UpdateProduct(db *gorm.DB, id uint, updates map[string]any) error
}

// Interface defines the inventory domain's use cases.
type Interface interface {
	// ListStockLevels is scoped to the authenticated caller's own
	// organization - branchID additionally restricts to one branch when
	// set (the caller's own branch from a pos-device token, or an
	// owner/service_center-supplied ?branch_id= - either way it's verified
	// against orgID before use, since StockLevel has no OrgID column of
	// its own to naturally gate a wrong value), same reasoning as
	// catalog.ListProducts. productID additionally restricts to one
	// product when set - no separate ownership check needed for it, since
	// a StockLevel row can only match both the branch scoping above and an
	// unrelated org's product if such a row genuinely exists, which it
	// won't.
	ListStockLevels(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]StockLevel, error)
	// ListLowStock is scoped exactly like ListStockLevels (same branchID
	// resolution) - "low" means the product's own qty at that branch is
	// <= its own Threshold (catalog.Product), so the repository joins to
	// products rather than needing a separate threshold input.
	ListLowStock(ctx context.Context, orgID uint, branchID *uint) ([]StockLevel, error)
	// ListInventoryLedger is scoped to the authenticated caller's own
	// organization - branchID/productID optionally narrow it further, same
	// filter shape as ListStockLevels. Unlike ListStockLevels, no
	// BranchLookup ownership check is needed for branchID, since
	// InventoryLedger carries its own OrgID (see the repository doc).
	ListInventoryLedger(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]InventoryLedger, error)
	// CreateStockAdjustment confirms in.ProductID belongs to orgID (via
	// ProductLookup), takes the product's own BranchID as the adjustment's
	// branch (a client never supplies one directly - see
	// CreateStockAdjustmentRequest), applies Delta to that product's
	// StockLevel (creating the row at 0 first if none exists yet), and
	// appends one InventoryLedger entry reflecting the movement - all in
	// one transaction, same shape as procurement.Service.CreateGoodsReceipt.
	// actorID is the authenticated caller's own user ID, never a
	// client-supplied one.
	CreateStockAdjustment(ctx context.Context, orgID uint, actorID uint, in CreateStockAdjustmentRequest) (*StockAdjustment, error)
	// ListStockTransfers is scoped to the authenticated caller's own
	// organization - branchID optionally narrows it to transfers where
	// that branch is either the sender or the receiver.
	ListStockTransfers(ctx context.Context, orgID uint, branchID *uint) ([]StockTransfer, error)
	// CreateStockTransfer confirms FromBranch and ToBranch both belong to
	// orgID and are different branches, and every item's ProductID belongs
	// to orgID AND to FromBranch specifically (a product only ever lives
	// at one branch, so that's the only branch it can be transferred out
	// of). Creates the StockTransfer and its StockTransferItems as one
	// atomic unit, starting at TransferStatusPending - no stock movement
	// happens yet, same reasoning as
	// procurement.Service.CreatePurchaseOrder not touching stock until a
	// GoodsReceipt is actually created. actorID is the authenticated
	// caller's own user ID, never a client-supplied one.
	CreateStockTransfer(ctx context.Context, orgID uint, actorID uint, in CreateStockTransferRequest) (*StockTransfer, error)
	// UpdateStockTransfer confirms the transfer exists AND belongs to
	// orgID, and blocks any update once it's already Completed or
	// Cancelled (terminal states). Moving Status to TransferStatusCompleted
	// is the one transition with a real side effect: for each item, it
	// requires FromBranch's current stock to cover Qty (rejecting the
	// whole transfer otherwise - no partial completion, no backorder),
	// then atomically decrements FromBranch's StockLevel and increments
	// ToBranch's StockLevel by Qty, and appends two InventoryLedger
	// entries per item (transfer_out at FromBranch, transfer_in at
	// ToBranch) - all in one transaction. Any other status transition
	// (in_transit, cancelled) is a plain field write with no stock effect.
	UpdateStockTransfer(ctx context.Context, orgID uint, id uint, in UpdateStockTransferRequest) (*StockTransfer, error)
}
