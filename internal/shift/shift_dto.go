package shift

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"shagan_pos/internal/sales"
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

// ShiftListQuery is `GET /shifts`'s filter as the handler read it. From / To are
// calendar dates (YYYY-MM-DD, inclusive) of the ORGANIZATION's own timezone,
// matched against opened_at; Status is "open" or "closed"; Discrepancy is
// "short" (counted cash below expected), "over" (above) or "any" (either) and
// only ever matches closed shifts. Page / PageSize are normalized by the
// service (default 25, at most 100).
type ShiftListQuery struct {
	BranchID    *uint
	From, To    *time.Time
	Status      string
	StaffID     *uint
	DeviceID    *uint
	Discrepancy string
	Page        int
	PageSize    int
}

// ShiftFilter is ShiftListQuery after the service has validated it and turned
// the dates into instants: opened_at in [Start, End).
type ShiftFilter struct {
	BranchID    *uint
	Start, End  *time.Time
	Status      *ShiftStatus
	StaffID     *uint
	DeviceID    *uint
	Discrepancy string
	Page        int
	PageSize    int
}

// Discrepancy filter values.
const (
	DiscrepancyShort = "short"
	DiscrepancyOver  = "over"
	DiscrepancyAny   = "any"
)

// ShiftFigures is what a shift sold and what should be in its drawer - THE one
// calculation, shared by `GET /shifts/:id/summary` and every `GET /shifts` row
// (see loadShiftFigures), so the list and the close-shift screen can never
// disagree. PaymentTotals holds only the methods that took a payment, like the
// summary always has.
type ShiftFigures struct {
	SalesCount      int64
	SalesTotal      decimal.Decimal
	PaymentTotals   map[string]decimal.Decimal
	CashRefunds     decimal.Decimal
	ExchangeCashIn  decimal.Decimal
	ExchangeCashOut decimal.Decimal
}

// ExpectedCash is what the drawer should hold: the opening float, plus cash
// sales, plus cash an exchange brought in, minus cash an exchange paid out and
// cash refunds. QR never touches the drawer.
func (f ShiftFigures) ExpectedCash(openingCash decimal.Decimal) decimal.Decimal {
	return openingCash.
		Add(f.PaymentTotals[string(sales.PaymentMethodCash)]).
		Add(f.ExchangeCashIn).Sub(f.ExchangeCashOut).Sub(f.CashRefunds)
}

// ShiftRow is one shift as the repository reads it for the list: the shift, the
// names behind its ids, its cash reconciliation row (nil for an open shift) and
// its live figures.
type ShiftRow struct {
	Shift
	BranchName   string
	DeviceName   string
	StaffName    string
	ClosedByName *string
	Cash         *ShiftReconciliation
	Figures      ShiftFigures
}

// ShiftListItem is one row of `GET /shifts`. Closed-only fields are null for an
// open shift. Money is a decimal string. ForceClosed is true only when someone
// other than the shift's own staff closed it.
type ShiftListItem struct {
	ID               uint                       `json:"id"`
	Status           ShiftStatus                `json:"status"`
	BranchID         uint                       `json:"branch_id"`
	BranchName       string                     `json:"branch_name"`
	DeviceID         uint                       `json:"device_id"`
	DeviceName       string                     `json:"device_name"`
	StaffID          uint                       `json:"staff_id"`
	StaffName        string                     `json:"staff_name"`
	ClosedByStaffID  *uint                      `json:"closed_by_staff_id"`
	ClosedByName     *string                    `json:"closed_by_name"`
	ForceClosed      bool                       `json:"force_closed"`
	OpenedAt         time.Time                  `json:"opened_at"`
	ClosedAt         *time.Time                 `json:"closed_at"`
	OpeningCash      decimal.Decimal            `json:"opening_cash"`
	SalesCount       int64                      `json:"sales_count"`
	SalesTotal       decimal.Decimal            `json:"sales_total"`
	PaymentTotals    map[string]decimal.Decimal `json:"payment_totals"`
	ExpectedCash     decimal.Decimal            `json:"expected_cash"`
	CountedCash      *decimal.Decimal           `json:"counted_cash"`
	CashDifference   *decimal.Decimal           `json:"cash_difference"`
	DifferenceReason *string                    `json:"difference_reason"`
}

// ShiftsPage is the `GET /shifts` response - same envelope as
// reports.TransactionsReport and sales.SalesPage.
type ShiftsPage struct {
	Shifts     []ShiftListItem `json:"shifts"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalCount int64           `json:"total_count"`
}
