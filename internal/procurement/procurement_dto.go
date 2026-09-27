package procurement

import (
	"time"

	"github.com/shopspring/decimal"
)

// CreateGoodsReceiptRequest is the request body for the endpoint that creates or updates a GoodsReceipt.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateGoodsReceiptRequest struct {
	PoID          uint            `json:"po_id" binding:"required"`
	ReceivedBy    uint            `json:"received_by" binding:"required"`
	ReceivedAt    time.Time       `json:"received_at" binding:"required"`
	TotalAmount   decimal.Decimal `json:"total_amount" binding:"required"`
	VarianceCount int             `json:"variance_count" binding:"required"`
}

// CreatePurchaseOrderRequest is the request body for the endpoint that creates or updates a PurchaseOrder.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreatePurchaseOrderRequest struct {
	PoNumber   string              `json:"po_number" binding:"required"`
	SupplierID uint                `json:"supplier_id" binding:"required"`
	Status     PurchaseOrderStatus `json:"status" binding:"required"`
	Total      decimal.Decimal     `json:"total" binding:"required"`
	CreatedBy  uint                `json:"created_by" binding:"required"`
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

// UpdatePurchaseOrderRequest is the request body for the endpoint that creates or updates a PurchaseOrder.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdatePurchaseOrderRequest struct {
	PoNumber   *string              `json:"po_number" binding:"omitempty"`
	SupplierID *uint                `json:"supplier_id" binding:"omitempty"`
	Status     *PurchaseOrderStatus `json:"status" binding:"omitempty"`
	Total      *decimal.Decimal     `json:"total" binding:"omitempty"`
	CreatedBy  *uint                `json:"created_by" binding:"omitempty"`
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
