package shift

import "context"

// Interface defines the shift domain's use cases.
type Interface interface {
	OpenShift(ctx context.Context, in OpenShiftRequest) (*Shift, error)
	GetCurrentShift(ctx context.Context, orgID, userID uint) (*Shift, error)
	GetShift(ctx context.Context, orgID, id uint) (*Shift, error)
	CloseShift(ctx context.Context, orgID, id uint) (*Shift, error)
	GetShiftSummary(ctx context.Context, id uint) (map[string]any, error)
	ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error)
	CreateDrawerEvent(ctx context.Context, in CreateDrawerEventRequest) (*DrawerEvent, error)
	ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error)
	ListExpenses(ctx context.Context) ([]Expense, error)
	CreateExpense(ctx context.Context, in CreateExpenseRequest) (*Expense, error)
	UpdateExpense(ctx context.Context, id uint, in UpdateExpenseRequest) (*Expense, error)
	DeleteExpense(ctx context.Context, id uint) error
}
