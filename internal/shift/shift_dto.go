package shift

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// AccessScope carries tenant boundaries derived from the signed access token.
// BranchID is set for branch-bound POS accounts and nil for org-wide accounts.
type AccessScope struct {
	OrgID    uint
	BranchID *uint
}

// ExpenseActor identifies the authenticated caller of
// UpdateExpense/DeleteExpense, and whether they may act on any expense in
// scope (rather than only their own). Two shapes, both derived from verified
// tokens, never client input: a staff member (StaffID, from X-Staff-Token;
// CanManageAny when their role grants access_backoffice - a Manager), or an
// org-wide Owner / Service Center account acting directly (UserID, from the
// bearer token; StaffID is 0 and CanManageAny is true - the Owner holds
// access_backoffice implicitly).
type ExpenseActor struct {
	StaffID      uint
	UserID       uint
	CanManageAny bool
}

// CreateDrawerEventRequest records why an authenticated branch opened the
// drawer. Shift ownership is checked against AccessScope; StaffID isn't
// binding:"required" - it's always overwritten from the verified
// X-Staff-Token before persistence (a client shouldn't need to send it at
// all), see cmd/api/shift.go's CreateDrawerEvent handler.
type CreateDrawerEventRequest struct {
	ShiftID uint       `json:"shift_id" binding:"required"`
	StaffID uint       `json:"staff_id"`
	Reason  string     `json:"reason" binding:"required"`
	SaleID  *uuid.UUID `json:"sale_id"`
}

// CreateExpenseRequest records a branch expense. A branch-bound POS caller's
// BranchID is derived from its signed access token by the API; org-wide callers
// may submit a branch, which the repository still verifies belongs to the org.
// CreatedBy isn't binding:"required" - it's always overwritten from the
// verified X-Staff-Token before persistence, same reasoning as
// CreateDrawerEventRequest.StaffID - see cmd/api/shift.go's CreateExpense handler.
// CreatedByUserID is the same idea for an Owner / Service Center logging an
// expense directly (no staff token): set from the bearer token only, never
// read from the body (json:"-"). Exactly one of the two is non-zero by the
// time the service sees it.
type CreateExpenseRequest struct {
	BranchID        uint            `json:"branch_id"`
	Date            time.Time       `json:"date" binding:"required"`
	Category        string          `json:"category" binding:"required"`
	Amount          decimal.Decimal `json:"amount" binding:"required"`
	CreatedBy       uint            `json:"created_by"`
	CreatedByUserID uint            `json:"-"`
}

// CloseShiftRequest is the request body for both `POST /shifts/:id/close`
// and `POST /shifts/:id/force-close`. Only cash is physically countable at
// close - card/QR/mobile-wallet payments settle electronically and are
// trusted to match what the system recorded (same as before this field
// existed). ClosingCash is what the person closing the shift actually
// counted in the drawer.
//
// Reason's requirement differs by which endpoint is used: for a normal
// close, it's only required when ClosingCash doesn't match the expected
// cash total (enforced in the repository, where the expected total is
// actually computed - see Repository.CloseShift). For a force-close, it's
// always required regardless of any cash match, since the override itself
// needs justification (e.g. "staff called in sick, closing on their
// behalf") - enforced in Service.ForceCloseShift.
type CloseShiftRequest struct {
	ClosingCash decimal.Decimal `json:"closing_cash" binding:"required"`
	Reason      string          `json:"reason"`
}

// OpenShiftRequest is the request body for opening a Shift. OpenedAt, ClosedAt,
// and Status remain for wire compatibility, but Service.OpenShift always
// replaces them with server-owned lifecycle values before persistence.
// StaffID isn't binding:"required" for the same reason - it's always
// overwritten from the verified X-Staff-Token, a shift always belongs to
// whoever actually PINs in to open it, never a client-supplied staff id (see
// cmd/api/shift.go's OpenShift handler). This is also what CloseShift later
// checks against: only that same staff member may close it.
type OpenShiftRequest struct {
	// OrgID is populated from the authenticated access token by the API and
	// is never accepted from JSON. Repository.OpenShift uses it for tenant
	// scoping in the same way as the identity repository's branch operations.
	OrgID       uint            `json:"-"`
	BranchID    uint            `json:"branch_id"`
	StaffID     uint            `json:"staff_id"`
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
