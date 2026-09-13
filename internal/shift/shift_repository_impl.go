package shift

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// OpenShift backs `POST /shifts`. Open shift with opening float
func (r *RepositoryImpl) OpenShift(ctx context.Context, in OpenShiftRequest) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// GetCurrentShift backs `GET /shifts/current`.
func (r *RepositoryImpl) GetCurrentShift(ctx context.Context) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// GetShift backs `GET /shifts/:id`.
func (r *RepositoryImpl) GetShift(ctx context.Context, id uint) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// CloseShift backs `POST /shifts/:id/close`. Writes reconciliation row(s) as a side effect
func (r *RepositoryImpl) CloseShift(ctx context.Context, id uint) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// GetShiftSummary backs `GET /shifts/:id/summary`. Printable summary
func (r *RepositoryImpl) GetShiftSummary(ctx context.Context, id uint) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ListShiftReconciliations backs `GET /shifts/:id/reconciliations`. Per-method breakdown
func (r *RepositoryImpl) ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error) {
	return nil, common.ErrNotImplemented
}

// CreateDrawerEvent backs `POST /drawer-events`. Cash drawer opened without a sale
func (r *RepositoryImpl) CreateDrawerEvent(ctx context.Context, in CreateDrawerEventRequest) (*DrawerEvent, error) {
	return nil, common.ErrNotImplemented
}

// ListDrawerEvents backs `GET /drawer-events`.
func (r *RepositoryImpl) ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error) {
	return nil, common.ErrNotImplemented
}

// ListExpenses backs `GET /expenses`.
func (r *RepositoryImpl) ListExpenses(ctx context.Context) ([]Expense, error) {
	return nil, common.ErrNotImplemented
}

// CreateExpense backs `POST /expenses`.
func (r *RepositoryImpl) CreateExpense(ctx context.Context, in CreateExpenseRequest) (*Expense, error) {
	return nil, common.ErrNotImplemented
}

// UpdateExpense backs `PATCH /expenses/:id`.
func (r *RepositoryImpl) UpdateExpense(ctx context.Context, id uint, in UpdateExpenseRequest) (*Expense, error) {
	return nil, common.ErrNotImplemented
}

// DeleteExpense backs `DELETE /expenses/:id`.
func (r *RepositoryImpl) DeleteExpense(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}
