package sales

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// CreateHeldSaleRequest is the request body for the endpoint that creates or updates a HeldSale.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateHeldSaleRequest struct {
	BranchID    uint            `json:"branch_id" binding:"required"`
	StaffID     uint            `json:"staff_id" binding:"required"`
	CustomerRef *uint           `json:"customer_ref"`
	Items       datatypes.JSON  `json:"items" binding:"required"`
	Discount    decimal.Decimal `json:"discount" binding:"required"`
	HeldAt      time.Time       `json:"held_at" binding:"required"`
}

// CreateSaleRequest is the request body for the endpoint that creates or updates a Sale.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateSaleRequest struct {
	ID          uuid.UUID       `json:"id" binding:"required"`
	OrgID       uint            `json:"org_id" binding:"required"`
	BranchID    uint            `json:"branch_id" binding:"required"`
	ShiftID     uint            `json:"shift_id" binding:"required"`
	StaffID     uint            `json:"staff_id" binding:"required"`
	DeviceID    uint            `json:"device_id" binding:"required"`
	CustomerID  *uint           `json:"customer_id"`
	Subtotal    decimal.Decimal `json:"subtotal" binding:"required"`
	Discount    decimal.Decimal `json:"discount" binding:"required"`
	Tax         decimal.Decimal `json:"tax" binding:"required"`
	Total       decimal.Decimal `json:"total" binding:"required"`
	Status      SaleStatus      `json:"status" binding:"required"`
	CompletedAt *time.Time      `json:"completed_at"`
	SyncedAt    *time.Time      `json:"synced_at"`
}
