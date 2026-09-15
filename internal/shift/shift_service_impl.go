package shift

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"shagan_pos/internal/common"
)

var maxOpeningCash = decimal.NewFromInt(100_000_000)

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

var _ Interface = (*Service)(nil)

func (s *Service) OpenShift(ctx context.Context, in OpenShiftRequest) (*Shift, error) {
	validationErrors := make([]common.FieldError, 0, 4)
	if in.BranchID == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "BranchID", Message: "required"})
	}
	if in.StaffID == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "StaffID", Message: "required"})
	}
	if in.DeviceID == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "DeviceID", Message: "required"})
	}
	if in.OpeningCash.IsNegative() {
		validationErrors = append(validationErrors, common.FieldError{Field: "OpeningCash", Message: "must be zero or greater"})
	} else if !in.OpeningCash.Round(2).Equal(in.OpeningCash) {
		validationErrors = append(validationErrors, common.FieldError{Field: "OpeningCash", Message: "must have at most 2 decimal places"})
	} else if in.OpeningCash.GreaterThanOrEqual(maxOpeningCash) {
		validationErrors = append(validationErrors, common.FieldError{Field: "OpeningCash", Message: "must be less than 100000000.00"})
	}
	if len(validationErrors) > 0 {
		return nil, common.ValidationError("validation error", validationErrors)
	}

	// Opening time and lifecycle state are facts established by this use case,
	// not values a client may choose. Keeping the fields on the DTO preserves
	// compatibility with older clients while deliberately ignoring them here.
	in.OpenedAt = s.now().UTC()
	in.ClosedAt = nil
	in.Status = ShiftStatusOpen

	return s.repo.OpenShift(ctx, in)
}

func (s *Service) GetCurrentShift(ctx context.Context, orgID, userID uint) (*Shift, error) {
	return s.repo.GetCurrentShift(ctx, orgID, userID)
}

func (s *Service) GetShift(ctx context.Context, orgID, id uint) (*Shift, error) {
	return s.repo.GetShift(ctx, orgID, id)
}

func (s *Service) CloseShift(ctx context.Context, orgID, id uint) (*Shift, error) {
	return s.repo.CloseShift(ctx, orgID, id, s.now().UTC())
}

func (s *Service) GetShiftSummary(ctx context.Context, id uint) (map[string]any, error) {
	return s.repo.GetShiftSummary(ctx, id)
}

func (s *Service) ListShiftReconciliations(ctx context.Context, id uint) ([]ShiftReconciliation, error) {
	return s.repo.ListShiftReconciliations(ctx, id)
}

func (s *Service) CreateDrawerEvent(ctx context.Context, in CreateDrawerEventRequest) (*DrawerEvent, error) {
	return s.repo.CreateDrawerEvent(ctx, in)
}

func (s *Service) ListDrawerEvents(ctx context.Context) ([]DrawerEvent, error) {
	return s.repo.ListDrawerEvents(ctx)
}

func (s *Service) ListExpenses(ctx context.Context) ([]Expense, error) {
	return s.repo.ListExpenses(ctx)
}

func (s *Service) CreateExpense(ctx context.Context, in CreateExpenseRequest) (*Expense, error) {
	return s.repo.CreateExpense(ctx, in)
}

func (s *Service) UpdateExpense(ctx context.Context, id uint, in UpdateExpenseRequest) (*Expense, error) {
	return s.repo.UpdateExpense(ctx, id, in)
}

func (s *Service) DeleteExpense(ctx context.Context, id uint) error {
	return s.repo.DeleteExpense(ctx, id)
}
