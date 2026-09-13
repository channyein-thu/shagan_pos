package shift

import "context"

// Interface defines the shift domain's use cases.
type Interface interface {
	OpenShift(ctx context.Context, in Shift) (*Shift, error)
	GetCurrentShift(ctx context.Context) (*Shift, error)
	GetShift(ctx context.Context, id uint) (*Shift, error)
	CloseShift(ctx context.Context, id uint) (*Shift, error)
	GetShiftSummary(ctx context.Context, id uint) (map[string]any, error)
	ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error)
	CreateDrawerEvent(ctx context.Context, in DrawerEvent) (*DrawerEvent, error)
	ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error)
	ListExpenses(ctx context.Context) ([]Expense, error)
	CreateExpense(ctx context.Context, in Expense) (*Expense, error)
	UpdateExpense(ctx context.Context, id uint, in Expense) (*Expense, error)
	DeleteExpense(ctx context.Context, id uint) error
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) OpenShift(ctx context.Context, in Shift) (*Shift, error) {
	return s.repo.OpenShift(ctx, in)
}

func (s *Service) GetCurrentShift(ctx context.Context) (*Shift, error) {
	return s.repo.GetCurrentShift(ctx)
}

func (s *Service) GetShift(ctx context.Context, id uint) (*Shift, error) {
	return s.repo.GetShift(ctx, id)
}

func (s *Service) CloseShift(ctx context.Context, id uint) (*Shift, error) {
	return s.repo.CloseShift(ctx, id)
}

func (s *Service) GetShiftSummary(ctx context.Context, id uint) (map[string]any, error) {
	return s.repo.GetShiftSummary(ctx, id)
}

func (s *Service) ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error) {
	return s.repo.ListShiftReconciliations(ctx, id)
}

func (s *Service) CreateDrawerEvent(ctx context.Context, in DrawerEvent) (*DrawerEvent, error) {
	return s.repo.CreateDrawerEvent(ctx, in)
}

func (s *Service) ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error) {
	return s.repo.ListDrawerEvents(ctx)
}

func (s *Service) ListExpenses(ctx context.Context) ([]Expense, error) {
	return s.repo.ListExpenses(ctx)
}

func (s *Service) CreateExpense(ctx context.Context, in Expense) (*Expense, error) {
	return s.repo.CreateExpense(ctx, in)
}

func (s *Service) UpdateExpense(ctx context.Context, id uint, in Expense) (*Expense, error) {
	return s.repo.UpdateExpense(ctx, id, in)
}

func (s *Service) DeleteExpense(ctx context.Context, id uint) error {
	return s.repo.DeleteExpense(ctx, id)
}
