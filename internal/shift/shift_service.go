package shift

import "context"

// Interface defines the shift domain's use cases.
type Interface interface {
	OpenShift(ctx context.Context, in OpenShiftRequest) (*Shift, error)
	GetCurrentShift(ctx context.Context, orgID, userID uint) (*Shift, error)
	GetShift(ctx context.Context, scope AccessScope, id uint) (*Shift, error)
	CloseShift(ctx context.Context, scope AccessScope, id uint, staffID uint, in CloseShiftRequest) (*Shift, error)
	GetShiftSummary(ctx context.Context, scope AccessScope, id uint) (map[string]any, error)
	ListShiftReconciliations(ctx context.Context, scope AccessScope, id uint) ([]ShiftReconciliation, error)
	CreateDrawerEvent(ctx context.Context, scope AccessScope, in CreateDrawerEventRequest) (*DrawerEvent, error)
	ListDrawerEvents(ctx context.Context, scope AccessScope) ([]DrawerEvent, error)
	ListExpenses(ctx context.Context, scope AccessScope) ([]Expense, error)
	CreateExpense(ctx context.Context, scope AccessScope, in CreateExpenseRequest) (*Expense, error)
	UpdateExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor, in UpdateExpenseRequest) (*Expense, error)
	DeleteExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor) error
}
