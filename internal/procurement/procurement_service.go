package procurement

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
)

// BranchLookup is the one identity operation procurement needs: confirming
// a client-supplied BranchID actually belongs to the caller's org before a
// PurchaseOrder gets placed by it (products are org-wide, see
// catalog.Product's doc, so the branch a goods receipt credits now comes
// from the order itself, not the product). identity.Repository already
// satisfies this signature - no adapter needed, same reasoning as
// catalog.BranchLookup.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
}

// ProductLookup is the one catalog operation procurement needs: confirming
// a client-supplied ProductID actually belongs to the caller's org before a
// PurchaseOrderItem gets attached to it. catalog.Repository already
// satisfies this signature - no adapter needed, same reasoning as
// inventory.ProductLookup.
type ProductLookup interface {
	GetProduct(ctx context.Context, orgID uint, id uint) (*catalog.Product, error)
	// UpdateProduct backs CreateGoodsReceipt's weighted-average cost blend
	// (see Product.CostPrice's own doc) - db is the same in-flight
	// transaction the stock/ledger writes participate in.
	UpdateProduct(db *gorm.DB, id uint, updates map[string]any) error
}

// InventoryWriter is what procurement needs from inventory: crediting a
// received product's stock at its branch and recording the ledger entry,
// same reasoning as catalog.BranchLookup. inventory.Repository already
// satisfies this signature - no adapter needed.
type InventoryWriter interface {
	GetStockLevel(db *gorm.DB, productID uint, branchID uint) (*inventory.StockLevel, error)
	CreateStockLevel(db *gorm.DB, level *inventory.StockLevel) error
	UpdateStockLevelQty(db *gorm.DB, id uint, qty int) error
	CreateInventoryLedgerEntry(db *gorm.DB, entry *inventory.InventoryLedger) error
}

// Interface defines the procurement domain's use cases.
type Interface interface {
	ListSuppliers(ctx context.Context, orgID uint) ([]Supplier, error)
	CreateSupplier(ctx context.Context, orgID uint, in CreateSupplierRequest) (*Supplier, error)
	// UpdateSupplier confirms the supplier exists AND belongs to orgID
	// before touching anything (not-found-not-forbidden, same reasoning as
	// catalog.UpdateCategory).
	UpdateSupplier(ctx context.Context, orgID uint, id uint, in UpdateSupplierRequest) (*Supplier, error)
	// DeleteSupplier confirms the supplier exists AND belongs to orgID
	// (same not-found-not-forbidden reasoning as UpdateSupplier), then
	// blocks the delete with common.ConflictError if any PurchaseOrder
	// still references it - deleting out from under a purchase order
	// would leave it pointing at a supplier that no longer exists, same
	// reasoning as catalog.DeleteCategory.
	DeleteSupplier(ctx context.Context, orgID uint, id uint) error
	// ListPurchaseOrders is scoped to the authenticated caller's own
	// organization - returns bare PurchaseOrder rows (headers only), same
	// reasoning as PurchaseOrderResult's doc.
	ListPurchaseOrders(ctx context.Context, orgID uint) ([]PurchaseOrder, error)
	// CreatePurchaseOrder confirms in.BranchID and in.SupplierID both belong
	// to orgID, and every item's ProductID does too (same
	// not-found-not-forbidden reasoning as catalog.CreateProduct's category
	// ownership check), computes Total from the items, generates PoNumber
	// as "PO-%04d" from orgID's existing purchase-order count plus one (so
	// two different staff members can never collide on a number they each
	// picked themselves), then creates the PurchaseOrder and its
	// PurchaseOrderItems together as one atomic unit of work: a purchase
	// order without its line items should never exist. Starts at
	// PurchaseOrderStatusSubmitted - see CreatePurchaseOrderRequest's doc.
	// createdBy is the authenticated caller's own user ID, never a
	// client-supplied one.
	CreatePurchaseOrder(ctx context.Context, orgID uint, createdBy uint, in CreatePurchaseOrderRequest) (*PurchaseOrder, error)
	// GetPurchaseOrder returns PurchaseOrderResult (the order plus its
	// items), confirming the order exists AND belongs to orgID
	// (not-found-not-forbidden, same reasoning as UpdateSupplier).
	GetPurchaseOrder(ctx context.Context, orgID uint, id uint) (*PurchaseOrderResult, error)
	// UpdatePurchaseOrder confirms the order exists AND belongs to orgID
	// before touching anything, and blocks any update at all once the
	// order is already Received or Cancelled (terminal states). Status can
	// move to any value except PurchaseOrderStatusReceived - see
	// UpdatePurchaseOrderRequest's doc.
	UpdatePurchaseOrder(ctx context.Context, orgID uint, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error)
	// CreateGoodsReceipt confirms the purchase order exists AND belongs to
	// orgID, and hasn't already been Received or Cancelled, then
	// reconciles each in.Items entry against its PurchaseOrderItem
	// (validating po_item_id actually belongs to this order, and requiring
	// variance_note whenever received_qty differs from ordered_qty),
	// atomically: creates the GoodsReceipt and its GoodsReceiptItems,
	// credits each product's StockLevel at the purchase order's own
	// BranchID by exactly received_qty (creating the row if none exists
	// yet - never the
	// ordered_qty, since only what actually arrived should ever increase
	// real stock), appends one InventoryLedger entry per item reflecting
	// that same real movement (the lost/short quantity is recorded via
	// GoodsReceiptItem.VarianceNote, traceable from the ledger via its
	// reference_id - not duplicated as a second, non-moving ledger row,
	// which would break every ledger row being a true running balance),
	// and marks the purchase order Received - all in one transaction.
	// receivedBy is the authenticated caller's own user ID, never a
	// client-supplied one.
	CreateGoodsReceipt(ctx context.Context, orgID uint, poID uint, receivedBy uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error)
}
