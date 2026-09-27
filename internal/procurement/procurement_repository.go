package procurement

import "context"

// Repository defines the procurement domain's persistence operations.
type Repository interface {
	// ListSuppliers backs `GET /suppliers`, scoped to the authenticated
	// caller's own organization - same reasoning as identity's org-scoped
	// lists (e.g. ListBranches).
	ListSuppliers(ctx context.Context, orgID uint) ([]Supplier, error)
	// CreateSupplier backs Service.CreateSupplier. Plain insert - GORM sets
	// the row's ID on the pointer it's given. Mapping the request/orgID
	// into a Supplier happens in the service, not here.
	CreateSupplier(ctx context.Context, supplier *Supplier) error
	// GetSupplier backs Service.UpdateSupplier/DeleteSupplier's
	// existence/ownership check. Scoped to orgID - returns
	// common.NotFoundError for a supplier that exists but belongs to a
	// different org, same as one that doesn't exist at all, so a caller
	// can never distinguish "not mine" from "doesn't exist" by probing IDs
	// (same reasoning as identity's GetBranch).
	GetSupplier(ctx context.Context, orgID uint, id uint) (*Supplier, error)
	// UpdateSupplier applies updates (already decided by the service -
	// which fields changed, in what shape) to the supplier identified by
	// id. Plain write - existence/ownership was already confirmed by a
	// prior GetSupplier call.
	UpdateSupplier(ctx context.Context, id uint, updates map[string]any) error
	// PurchaseOrdersExistForSupplier backs Service.DeleteSupplier's
	// referential-integrity check - a plain existence query. Whether that
	// should block the delete is the service's call, not this one's, same
	// reasoning as catalog.ProductsExistForCategory.
	PurchaseOrdersExistForSupplier(ctx context.Context, supplierID uint) (bool, error)
	// DeleteSupplier is a hard delete - Supplier has no status field to
	// deactivate instead (unlike Staff/Branch/Device). Existence/ownership
	// was already confirmed by a prior GetSupplier call.
	DeleteSupplier(ctx context.Context, id uint) error
	ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error)
	CreatePurchaseOrder(ctx context.Context, in CreatePurchaseOrderRequest) (*PurchaseOrder, error)
	GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error)
	UpdatePurchaseOrder(ctx context.Context, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error)
	CreateGoodsReceipt(ctx context.Context, id uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error)
}
