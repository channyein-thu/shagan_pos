package shift

import (
	"time"

	"github.com/shopspring/decimal"
)

// CreateDrawerEventRequest is the request body for the endpoint that creates or updates a DrawerEvent.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateDrawerEventRequest struct {
	ShiftID uint   `json:"shift_id" binding:"required"`
	StaffID uint   `json:"staff_id" binding:"required"`
	Reason  string `json:"reason" binding:"required"`
	SaleID  *uint  `json:"sale_id"`
}

// CreateExpenseRequest is the request body for the endpoint that creates or updates a Expense.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateExpenseRequest struct {
	BranchID  uint            `json:"branch_id" binding:"required"`
	Date      time.Time       `json:"date" binding:"required"`
	Category  string          `json:"category" binding:"required"`
	Amount    decimal.Decimal `json:"amount" binding:"required"`
	CreatedBy uint            `json:"created_by" binding:"required"`
}

// OpenShiftRequest is the request body for the endpoint that creates or updates a Shift.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type OpenShiftRequest struct {
	BranchID    uint            `json:"branch_id" binding:"required"`
	StaffID     uint            `json:"staff_id" binding:"required"`
	DeviceID    uint            `json:"device_id" binding:"required"`
	OpenedAt    time.Time       `json:"opened_at" binding:"required"`
	OpeningCash decimal.Decimal `json:"opening_cash" binding:"required"`
	ClosedAt    *time.Time      `json:"closed_at"`
	Status      ShiftStatus     `json:"status" binding:"required"`
}

// UpdateExpenseRequest is the request body for the endpoint that creates or updates a Expense.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateExpenseRequest struct {
	BranchID  *uint            `json:"branch_id" binding:"omitempty"`
	Date      *time.Time       `json:"date" binding:"omitempty"`
	Category  *string          `json:"category" binding:"omitempty"`
	Amount    *decimal.Decimal `json:"amount" binding:"omitempty"`
	CreatedBy *uint            `json:"created_by" binding:"omitempty"`
}
