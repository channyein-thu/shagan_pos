package procurement

import (
	"context"

	"gorm.io/gorm"
)

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
	// ListPurchaseOrders backs `GET /purchase-orders`, scoped to the
	// authenticated caller's own organization - PurchaseOrder carries no
	// OrgID of its own (only SupplierID), so this is scoped via a subquery
	// against this org's own Suppliers, same reasoning as identity's
	// orgBranchIDs helper (both are same-domain subqueries, not a
	// cross-domain dependency).
	ListPurchaseOrders(ctx context.Context, orgID uint) ([]PurchaseOrder, error)
	// CountPurchaseOrders backs Service.CreatePurchaseOrder's po_number
	// generation - the count of orgID's existing purchase orders becomes
	// the next sequence number (see Service.CreatePurchaseOrder's doc). db
	// is the same in-flight transaction the insert that follows
	// participates in, keeping the read and the write as close together as
	// this domain's other count-then-insert steps (same
	// db-is-either-plain-or-in-flight-transaction reasoning as
	// CreatePurchaseOrder below).
	CountPurchaseOrders(db *gorm.DB, orgID uint) (int64, error)
	// CreatePurchaseOrder backs Service.CreatePurchaseOrder's first step.
	// Plain insert - GORM sets the row's ID on the pointer it's given.
	// Mapping the request into a PurchaseOrder, and validating it, happens
	// in the service, not here. db is either the repository's normal
	// connection or an in-flight transaction handed down by the caller -
	// Service.CreatePurchaseOrder runs this and CreatePurchaseOrderItems
	// inside one db.Transaction, since a purchase order without its line
	// items should never exist (same reasoning as Combo/ComboItem).
	CreatePurchaseOrder(db *gorm.DB, po *PurchaseOrder) error
	// CreatePurchaseOrderItems backs Service.CreatePurchaseOrder's second
	// step. Plain slice-insert, same
	// db-is-either-plain-or-in-flight-transaction reasoning as
	// CreatePurchaseOrder above.
	CreatePurchaseOrderItems(db *gorm.DB, items []PurchaseOrderItem) error
	// GetPurchaseOrder backs `GET /purchase-orders/:id` and every other
	// existence/ownership check in this domain. Scoped to orgID via the
	// same supplier subquery as ListPurchaseOrders - returns
	// common.NotFoundError for a purchase order that exists but belongs to
	// a different org, same as one that doesn't exist at all, so a caller
	// can never distinguish "not mine" from "doesn't exist" by probing
	// IDs.
	GetPurchaseOrder(ctx context.Context, orgID uint, id uint) (*PurchaseOrder, error)
	// ListPurchaseOrderItemsByPoID backs Service.GetPurchaseOrder's item
	// attachment and Service.CreateGoodsReceipt's line-item resolution - a
	// plain query, no business decision about which purchase order the
	// caller is allowed to see (that's already been decided by a prior
	// GetPurchaseOrder call).
	ListPurchaseOrderItemsByPoID(ctx context.Context, poID uint) ([]PurchaseOrderItem, error)
	// UpdatePurchaseOrder applies updates (already decided by the service -
	// which fields changed, in what shape) to the purchase order
	// identified by id. Plain write - existence/ownership was already
	// confirmed by a prior GetPurchaseOrder call.
	UpdatePurchaseOrder(ctx context.Context, id uint, updates map[string]any) error
	// UpdatePurchaseOrderStatus backs Service.CreateGoodsReceipt's
	// final step - marking the purchase order Received once its goods
	// receipt has been recorded. Plain write, same
	// db-is-either-plain-or-in-flight-transaction reasoning as
	// CreatePurchaseOrder above.
	UpdatePurchaseOrderStatus(db *gorm.DB, id uint, status PurchaseOrderStatus) error
	// CreateGoodsReceipt backs Service.CreateGoodsReceipt's first step.
	// Plain insert, same db-is-either-plain-or-in-flight-transaction
	// reasoning as CreatePurchaseOrder above.
	CreateGoodsReceipt(db *gorm.DB, receipt *GoodsReceipt) error
	// CreateGoodsReceiptItems backs Service.CreateGoodsReceipt's second
	// step. Plain slice-insert, same
	// db-is-either-plain-or-in-flight-transaction reasoning as
	// CreatePurchaseOrder above.
	CreateGoodsReceiptItems(db *gorm.DB, items []GoodsReceiptItem) error
}
