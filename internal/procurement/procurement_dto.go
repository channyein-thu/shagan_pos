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

// CreateSupplierRequest is the request body for the endpoint that creates or updates a Supplier.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateSupplierRequest struct {
	OrgID       uint      `json:"org_id" binding:"required"`
	Name        string    `json:"name" binding:"required"`
	Phone       string    `json:"phone" binding:"required"`
	Address     string    `json:"address" binding:"required"`
	LastOrderAt time.Time `json:"last_order_at" binding:"required"`
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

// UpdateSupplierRequest is the request body for the endpoint that creates or updates a Supplier.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateSupplierRequest struct {
	OrgID       *uint      `json:"org_id" binding:"omitempty"`
	Name        *string    `json:"name" binding:"omitempty"`
	Phone       *string    `json:"phone" binding:"omitempty"`
	Address     *string    `json:"address" binding:"omitempty"`
	LastOrderAt *time.Time `json:"last_order_at" binding:"omitempty"`
}
