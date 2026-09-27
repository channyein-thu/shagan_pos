package inventory

import (
	"context"

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
	ListLowStock(ctx context.Context) ([]StockLevel, error)
	ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error)
	CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error)
	ListStockTransfers(ctx context.Context) ([]StockTransfer, error)
	CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error)
	UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error)
}
