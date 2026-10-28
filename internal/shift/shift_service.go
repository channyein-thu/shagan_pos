package shift

import (
	"context"

	"shagan_pos/internal/identity"
)

// OrganizationLookup is what shift needs from identity: the org's timezone,
// which defines the calendar day behind GET /shifts' from / to dates. Only read
// when a date bound is supplied. identity.Repository already satisfies this
// signature - no adapter needed.
type OrganizationLookup interface {
	GetOrganization(ctx context.Context, id uint) (*identity.Organization, error)
}

// Interface defines the shift domain's use cases.
type Interface interface {
	OpenShift(ctx context.Context, in OpenShiftRequest) (*Shift, error)
	GetCurrentShift(ctx context.Context, orgID, userID uint) (*Shift, error)
	GetShift(ctx context.Context, scope AccessScope, id uint) (*Shift, error)
	CloseShift(ctx context.Context, scope AccessScope, id uint, staffID uint, in CloseShiftRequest) (*Shift, error)
	// ForceCloseShift is CloseShift's Manager-only escape hatch - Reason is
	// always required, not just on a cash mismatch - see Repository.ForceCloseShift.
	ForceCloseShift(ctx context.Context, scope AccessScope, id uint, closedByStaffID uint, in CloseShiftRequest) (*Shift, error)
	GetShiftSummary(ctx context.Context, scope AccessScope, id uint) (map[string]any, error)
	ListShiftReconciliations(ctx context.Context, scope AccessScope, id uint) ([]ShiftReconciliation, error)
	// CreateDrawerEvent requires canOpenDrawerNoSale when in.SaleID is nil -
	// see Service.CreateDrawerEvent.
	CreateDrawerEvent(ctx context.Context, scope AccessScope, canOpenDrawerNoSale bool, in CreateDrawerEventRequest) (*DrawerEvent, error)
	// ListShifts is `GET /shifts`: a page of shifts, newest first (opened_at),
	// each with who worked it, what it sold, and whether the cash was short or
	// over - the figures are GetShiftSummary's own calculation. 400 for a bad
	// status or discrepancy. A branch-bound scope (a manager at a till) is
	// limited to its own branch: another branch_id is a 403.
	ListShifts(ctx context.Context, scope AccessScope, q ShiftListQuery) (*ShiftsPage, error)
	// ListDrawerEvents / ListExpenses return everything in scope, or only one
	// shift's when shiftID is set.
	ListDrawerEvents(ctx context.Context, scope AccessScope, shiftID *uint) ([]DrawerEvent, error)
	ListExpenses(ctx context.Context, scope AccessScope, shiftID *uint) ([]Expense, error)
	CreateExpense(ctx context.Context, scope AccessScope, in CreateExpenseRequest) (*Expense, error)
	UpdateExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor, in UpdateExpenseRequest) (*Expense, error)
	DeleteExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor) error
}
