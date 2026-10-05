package procurement

import (
	"time"

	"github.com/shopspring/decimal"
)

// CreateGoodsReceiptRequest is the request body for `POST
// /purchase-orders/:id/receipts`. PoID isn't here - it's the :id path
// param. ReceivedBy is deliberately not here - it's the authenticated
// caller's own user ID, never a client-supplied one, same reasoning as
// inventory's actor handling. TotalAmount/VarianceCount are deliberately
// not here either - both are computed by the service from Items (sum of
// received_qty*unit_cost, and total shortfall units), never client-supplied,
// same reasoning as CreateComboRequest's Price staying out of the request
// for computed fields. Items must contain at least one entry with no
// duplicate po_item_id.
type CreateGoodsReceiptRequest struct {
	ReceivedAt time.Time                       `json:"received_at" binding:"required"`
	Items      []CreateGoodsReceiptItemRequest `json:"items" binding:"required,min=1,unique=PoItemID,dive"`
}

// CreateGoodsReceiptItemRequest is one reconciled line within
// CreateGoodsReceiptRequest - po_item_id must belong to the purchase order
// being received. received_qty carries no binding tag - 0 is a legitimate
// value (the item was a total loss), unlike CreateStockAdjustmentRequest's
// Delta where 0 is never meaningful; the service instead requires
// variance_note whenever received_qty differs from the item's ordered_qty.
type CreateGoodsReceiptItemRequest struct {
	PoItemID     uint   `json:"po_item_id" binding:"required"`
	ReceivedQty  int    `json:"received_qty"`
	VarianceNote string `json:"variance_note" binding:"omitempty"`
}

// CreatePurchaseOrderRequest is the request body for `POST
// /purchase-orders`. BranchID is the branch placing the order (re-verified
// against the caller's own org) - since Product is org-wide (see
// catalog.Product's doc), this is also the branch CreateGoodsReceipt later
// credits stock to. SupplierID is likewise a legitimate client choice,
// re-verified against the caller's own org by the service. Status is deliberately not
// here - a new purchase order always starts at PurchaseOrderStatusSubmitted;
// moving it along the lifecycle happens via UpdatePurchaseOrder (or, for
// the Received transition specifically, only via CreateGoodsReceipt, which
// requires an actual reconciled receipt to exist first). Total is
// deliberately not here either - it's computed by the service from Items
// (sum of ordered_qty*unit_cost), never client-supplied. CreatedBy is
// deliberately not here - it's the authenticated caller's own user ID,
// never a client-supplied one. Items must contain at least one entry with
// no duplicate product_id - same reasoning as CreateComboRequest's Items.
type CreatePurchaseOrderRequest struct {
	BranchID   uint                             `json:"branch_id" binding:"required"`
	PoNumber   string                           `json:"po_number" binding:"required"`
	SupplierID uint                             `json:"supplier_id" binding:"required"`
	Items      []CreatePurchaseOrderItemRequest `json:"items" binding:"required,min=1,unique=ProductID,dive"`
}

// CreatePurchaseOrderItemRequest is one bundled product within
// CreatePurchaseOrderRequest. UnitCost carries no binding tag - same
// reasoning as CreateProductRequest's Price: decimal.Decimal's zero value
// only catches an omitted field, not an explicit "unit_cost": 0, so
// Service.CreatePurchaseOrder validates it's actually positive.
type CreatePurchaseOrderItemRequest struct {
	ProductID  uint            `json:"product_id" binding:"required"`
	OrderedQty int             `json:"ordered_qty" binding:"required,min=1"`
	UnitCost   decimal.Decimal `json:"unit_cost"`
}

// CreateSupplierRequest is the request body for `POST /suppliers`. OrgID is
// deliberately not here - a supplier always belongs to the authenticated
// caller's own organization, never a client-supplied org, same reasoning as
// catalog.CreateCategoryRequest. LastOrderAt is deliberately not here
// either - it's a system-derived timestamp (set to the creation time, then
// later updated whenever a real PurchaseOrder is placed with this
// supplier), never a client-supplied one, same reasoning as
// identity.Device.LastSeenAt.
type CreateSupplierRequest struct {
	Name    string `json:"name" binding:"required"`
	Phone   string `json:"phone" binding:"required"`
	Address string `json:"address" binding:"required"`
}

// UpdatePurchaseOrderRequest is the request body for `PATCH
// /purchase-orders/:id`. SupplierID, if present, is re-verified against
// the caller's own org by the service, same reasoning as
// CreatePurchaseOrderRequest. Status can move to any value except
// PurchaseOrderStatusReceived - that transition only happens via
// CreateGoodsReceipt, which requires an actual reconciled receipt to
// exist first; see Service.UpdatePurchaseOrder. Total/CreatedBy are
// deliberately not here - Total is derived from the order's items (not
// editable by this endpoint, which doesn't touch items), and CreatedBy is
// an immutable audit field, same reasoning as CreatedAt.
type UpdatePurchaseOrderRequest struct {
	PoNumber   *string              `json:"po_number" binding:"omitempty"`
	SupplierID *uint                `json:"supplier_id" binding:"omitempty"`
	Status     *PurchaseOrderStatus `json:"status" binding:"omitempty"`
}

// UpdateSupplierRequest is the request body for `PATCH /suppliers/:id`.
// OrgID is deliberately not here - a supplier can never be reassigned to a
// different organization via a client update, same reasoning as
// catalog.UpdateCategoryRequest. LastOrderAt is deliberately not here
// either - same reasoning as CreateSupplierRequest's doc; it isn't
// client-editable at all, only ever system-updated when a real
// PurchaseOrder is placed.
type UpdateSupplierRequest struct {
	Name    *string `json:"name" binding:"omitempty"`
	Phone   *string `json:"phone" binding:"omitempty"`
	Address *string `json:"address" binding:"omitempty"`
}

// PurchaseOrderResult is what GetPurchaseOrder returns - the PurchaseOrder
// row plus its items. ListPurchaseOrders returns bare PurchaseOrder rows
// (headers only) - a list view doesn't need every line item repeated for
// every row, same list-vs-detail distinction as most REST APIs.
type PurchaseOrderResult struct {
	PurchaseOrder
	Items []PurchaseOrderItem `json:"items"`
}
