package shift

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
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

func (s *Service) GetShift(ctx context.Context, scope AccessScope, id uint) (*Shift, error) {
	return s.repo.GetShift(ctx, scope, id)
}

func (s *Service) CloseShift(ctx context.Context, scope AccessScope, id uint, staffID uint, in CloseShiftRequest) (*Shift, error) {
	if in.ClosingCash.IsNegative() {
		return nil, common.ValidationError("validation error", []common.FieldError{{Field: "ClosingCash", Message: "must be zero or greater"}})
	} else if !in.ClosingCash.Round(2).Equal(in.ClosingCash) {
		return nil, common.ValidationError("validation error", []common.FieldError{{Field: "ClosingCash", Message: "must have at most 2 decimal places"}})
	} else if in.ClosingCash.GreaterThanOrEqual(maxOpeningCash) {
		return nil, common.ValidationError("validation error", []common.FieldError{{Field: "ClosingCash", Message: "must be less than 100000000.00"}})
	}
	in.Reason = strings.TrimSpace(in.Reason)
	return s.repo.CloseShift(ctx, scope, id, s.now().UTC(), staffID, in)
}

func (s *Service) GetShiftSummary(ctx context.Context, scope AccessScope, id uint) (map[string]any, error) {
	return s.repo.GetShiftSummary(ctx, scope, id)
}

func (s *Service) ListShiftReconciliations(ctx context.Context, scope AccessScope, id uint) ([]ShiftReconciliation, error) {
	return s.repo.ListShiftReconciliations(ctx, scope, id)
}

func (s *Service) CreateDrawerEvent(ctx context.Context, scope AccessScope, in CreateDrawerEventRequest) (*DrawerEvent, error) {
	validationErrors := make([]common.FieldError, 0, 3)
	if in.ShiftID == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "ShiftID", Message: "required"})
	}
	if in.StaffID == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "StaffID", Message: "required"})
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" {
		validationErrors = append(validationErrors, common.FieldError{Field: "Reason", Message: "required"})
	}
	if in.SaleID != nil && *in.SaleID == uuid.Nil {
		validationErrors = append(validationErrors, common.FieldError{Field: "SaleID", Message: "must not be the nil UUID"})
	}
	if len(validationErrors) > 0 {
		return nil, common.ValidationError("validation error", validationErrors)
	}
	return s.repo.CreateDrawerEvent(ctx, scope, in)
}

func (s *Service) ListDrawerEvents(ctx context.Context, scope AccessScope) ([]DrawerEvent, error) {
	return s.repo.ListDrawerEvents(ctx, scope)
}

func (s *Service) ListExpenses(ctx context.Context, scope AccessScope) ([]Expense, error) {
	return s.repo.ListExpenses(ctx, scope)
}

func (s *Service) CreateExpense(ctx context.Context, scope AccessScope, in CreateExpenseRequest) (*Expense, error) {
	validationErrors := validateExpenseInput(in.BranchID, in.Date, in.Category, in.Amount, in.CreatedBy)
	if len(validationErrors) > 0 {
		return nil, common.ValidationError("validation error", validationErrors)
	}
	in.Category = strings.TrimSpace(in.Category)
	return s.repo.CreateExpense(ctx, scope, in)
}

func (s *Service) UpdateExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor, in UpdateExpenseRequest) (*Expense, error) {
	validationErrors := make([]common.FieldError, 0, 5)
	if in.BranchID == nil && in.Date == nil && in.Category == nil && in.Amount == nil && in.CreatedBy == nil {
		validationErrors = append(validationErrors, common.FieldError{Field: "body", Message: "at least one field is required"})
	}
	if in.BranchID != nil && *in.BranchID == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "BranchID", Message: "required"})
	}
	if in.Date != nil && in.Date.IsZero() {
		validationErrors = append(validationErrors, common.FieldError{Field: "Date", Message: "required"})
	}
	if in.Category != nil {
		trimmed := strings.TrimSpace(*in.Category)
		if trimmed == "" {
			validationErrors = append(validationErrors, common.FieldError{Field: "Category", Message: "required"})
		} else {
			in.Category = &trimmed
		}
	}
	if in.Amount != nil {
		validationErrors = append(validationErrors, validatePositiveMoney("Amount", *in.Amount)...)
	}
	if in.CreatedBy != nil && *in.CreatedBy == 0 {
		validationErrors = append(validationErrors, common.FieldError{Field: "CreatedBy", Message: "required"})
	}
	if len(validationErrors) > 0 {
		return nil, common.ValidationError("validation error", validationErrors)
	}
	return s.repo.UpdateExpense(ctx, scope, id, actor, in)
}

func (s *Service) DeleteExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor) error {
	return s.repo.DeleteExpense(ctx, scope, id, actor)
}

func validateExpenseInput(branchID uint, date time.Time, category string, amount decimal.Decimal, createdBy uint) []common.FieldError {
	errs := make([]common.FieldError, 0, 5)
	if branchID == 0 {
		errs = append(errs, common.FieldError{Field: "BranchID", Message: "required"})
	}
	if date.IsZero() {
		errs = append(errs, common.FieldError{Field: "Date", Message: "required"})
	}
	if strings.TrimSpace(category) == "" {
		errs = append(errs, common.FieldError{Field: "Category", Message: "required"})
	}
	errs = append(errs, validatePositiveMoney("Amount", amount)...)
	if createdBy == 0 {
		errs = append(errs, common.FieldError{Field: "CreatedBy", Message: "required"})
	}
	return errs
}

func validatePositiveMoney(field string, amount decimal.Decimal) []common.FieldError {
	switch {
	case !amount.IsPositive():
		return []common.FieldError{{Field: field, Message: "must be greater than zero"}}
	case !amount.Round(2).Equal(amount):
		return []common.FieldError{{Field: field, Message: "must have at most 2 decimal places"}}
	case amount.GreaterThanOrEqual(maxOpeningCash):
		return []common.FieldError{{Field: field, Message: "must be less than 100000000.00"}}
	default:
		return nil
	}
}
