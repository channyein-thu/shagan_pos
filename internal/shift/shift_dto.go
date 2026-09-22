package shift

import (
	"time"

	"github.com/shopspring/decimal"
)

// AccessScope carries tenant boundaries derived from the signed access token.
// BranchID is set for branch-bound POS accounts and nil for org-wide accounts.
type AccessScope struct {
	OrgID    uint
	BranchID *uint
}

// CreateDrawerEventRequest records why an authenticated branch opened the
// drawer. Shift ownership is checked against AccessScope; StaffID is checked
// against the shift's branch before persistence.
type CreateDrawerEventRequest struct {
	ShiftID uint   `json:"shift_id" binding:"required"`
	StaffID uint   `json:"staff_id" binding:"required"`
	Reason  string `json:"reason" binding:"required"`
	SaleID  *uint  `json:"sale_id"`
}

// CreateExpenseRequest records a branch expense. A branch-bound POS caller's
// BranchID is derived from its signed access token by the API; org-wide callers
// may submit a branch, which the repository still verifies belongs to the org.
type CreateExpenseRequest struct {
	BranchID  uint            `json:"branch_id"`
	Date      time.Time       `json:"date" binding:"required"`
	Category  string          `json:"category" binding:"required"`
	Amount    decimal.Decimal `json:"amount" binding:"required"`
	CreatedBy uint            `json:"created_by" binding:"required"`
}

// OpenShiftRequest is the request body for opening a Shift. OpenedAt, ClosedAt,
// and Status remain for wire compatibility, but Service.OpenShift always
// replaces them with server-owned lifecycle values before persistence.
type OpenShiftRequest struct {
	// OrgID is populated from the authenticated access token by the API and
	// is never accepted from JSON. Repository.OpenShift uses it for tenant
	// scoping in the same way as the identity repository's branch operations.
	OrgID       uint            `json:"-"`
	BranchID    uint            `json:"branch_id"`
	StaffID     uint            `json:"staff_id" binding:"required"`
	DeviceID    uint            `json:"device_id" binding:"required"`
	OpenedAt    time.Time       `json:"opened_at"`
	OpeningCash decimal.Decimal `json:"opening_cash" binding:"required"`
	ClosedAt    *time.Time      `json:"closed_at"`
	Status      ShiftStatus     `json:"status"`
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
