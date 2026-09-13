package shift

import "context"

// Repository defines the shift domain's persistence operations.
type Repository interface {
	OpenShift(ctx context.Context, in OpenShiftRequest) (*Shift, error)
	GetCurrentShift(ctx context.Context) (*Shift, error)
	GetShift(ctx context.Context, id uint) (*Shift, error)
	CloseShift(ctx context.Context, id uint) (*Shift, error)
	GetShiftSummary(ctx context.Context, id uint) (map[string]any, error)
	ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error)
	CreateDrawerEvent(ctx context.Context, in CreateDrawerEventRequest) (*DrawerEvent, error)
	ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error)
	ListExpenses(ctx context.Context) ([]Expense, error)
	CreateExpense(ctx context.Context, in CreateExpenseRequest) (*Expense, error)
	UpdateExpense(ctx context.Context, id uint, in UpdateExpenseRequest) (*Expense, error)
	DeleteExpense(ctx context.Context, id uint) error
}
