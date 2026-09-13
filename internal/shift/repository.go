package shift

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// OpenShift backs `POST /shifts`. Open shift with opening float
func (r *Repository) OpenShift(ctx context.Context, in Shift) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// GetCurrentShift backs `GET /shifts/current`.
func (r *Repository) GetCurrentShift(ctx context.Context) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// GetShift backs `GET /shifts/:id`.
func (r *Repository) GetShift(ctx context.Context, id uint) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// CloseShift backs `POST /shifts/:id/close`. Writes reconciliation row(s) as a side effect
func (r *Repository) CloseShift(ctx context.Context, id uint) (*Shift, error) {
	return nil, common.ErrNotImplemented
}

// GetShiftSummary backs `GET /shifts/:id/summary`. Printable summary
func (r *Repository) GetShiftSummary(ctx context.Context, id uint) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ListShiftReconciliations backs `GET /shifts/:id/reconciliations`. Per-method breakdown
func (r *Repository) ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error) {
	return nil, common.ErrNotImplemented
}

// CreateDrawerEvent backs `POST /drawer-events`. Cash drawer opened without a sale
func (r *Repository) CreateDrawerEvent(ctx context.Context, in DrawerEvent) (*DrawerEvent, error) {
	return nil, common.ErrNotImplemented
}

// ListDrawerEvents backs `GET /drawer-events`.
func (r *Repository) ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error) {
	return nil, common.ErrNotImplemented
}

// ListExpenses backs `GET /expenses`.
func (r *Repository) ListExpenses(ctx context.Context) ([]Expense, error) {
	return nil, common.ErrNotImplemented
}

// CreateExpense backs `POST /expenses`.
func (r *Repository) CreateExpense(ctx context.Context, in Expense) (*Expense, error) {
	return nil, common.ErrNotImplemented
}

// UpdateExpense backs `PATCH /expenses/:id`.
func (r *Repository) UpdateExpense(ctx context.Context, id uint, in Expense) (*Expense, error) {
	return nil, common.ErrNotImplemented
}

// DeleteExpense backs `DELETE /expenses/:id`.
func (r *Repository) DeleteExpense(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}
