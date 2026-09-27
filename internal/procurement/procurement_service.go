package procurement

import "context"

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
	ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error)
	CreatePurchaseOrder(ctx context.Context, in CreatePurchaseOrderRequest) (*PurchaseOrder, error)
	GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error)
	UpdatePurchaseOrder(ctx context.Context, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error)
	CreateGoodsReceipt(ctx context.Context, id uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error)
}
