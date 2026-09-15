package shift

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/sales"
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
	var opened Shift
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Locking the staff and device rows serializes competing attempts to
		// open a shift for the same cashier/terminal. The open-shift lookup is
		// intentionally done after both locks are held.
		var branch identity.Branch
		if err := tx.Where("id = ? AND org_id = ?", in.BranchID, in.OrgID).
			First(&branch).Error; err != nil {
			return openShiftLookupError(err, "branch not found")
		}
		if branch.Status != identity.BranchStatusActive {
			return common.ConflictError("branch is not active")
		}

		var staff identity.Staff
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND branch_id = ?", in.StaffID, in.BranchID).
			First(&staff).Error
		if err != nil {
			return openShiftLookupError(err, "staff not found in branch")
		}
		if staff.Status != identity.StaffStatusActive {
			return common.ConflictError("staff is not active")
		}

		var device identity.Device
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND branch_id = ?", in.DeviceID, in.BranchID).
			First(&device).Error
		if err != nil {
			return openShiftLookupError(err, "device not found in branch")
		}
		if device.Status != identity.DeviceStatusActive {
			return common.ConflictError("device is not active")
		}

		var existing Shift
		err = tx.Where(
			"status = ? AND (staff_id = ? OR device_id = ?)",
			ShiftStatusOpen,
			in.StaffID,
			in.DeviceID,
		).First(&existing).Error
		switch {
		case err == nil:
			return common.ConflictError("staff or device already has an open shift")
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}

		opened = Shift{
			BranchID:    in.BranchID,
			StaffID:     in.StaffID,
			DeviceID:    in.DeviceID,
			OpenedAt:    in.OpenedAt,
			OpeningCash: in.OpeningCash,
			ClosedAt:    nil,
			Status:      ShiftStatusOpen,
		}
		return tx.Create(&opened).Error
	})
	if err != nil {
		return nil, err
	}

	return &opened, nil
}

func openShiftLookupError(err error, message string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return common.NotFoundError(message)
	}
	return err
}

// GetCurrentShift backs `GET /shifts/current`.
func (r *RepositoryImpl) GetCurrentShift(ctx context.Context, orgID, userID uint) (*Shift, error) {
	var user identity.User
	err := r.db.WithContext(ctx).
		Where("id = ? AND org_id = ?", userID, orgID).
		First(&user).Error
	if err != nil {
		return nil, openShiftLookupError(err, "user not found")
	}

	// "Current" is terminal-specific. Owner and service-center accounts are
	// organization-wide and have no paired device, so there is no unambiguous
	// current shift for those account types.
	if user.AccountType != identity.AccountTypePos || user.BranchID == nil || user.DeviceID == nil {
		return nil, common.ForbiddenError("current shift is only available to a paired POS account")
	}

	var current Shift
	err = r.db.WithContext(ctx).
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where(
			"branches.org_id = ? AND shifts.branch_id = ? AND shifts.device_id = ? AND shifts.status = ?",
			orgID,
			*user.BranchID,
			*user.DeviceID,
			ShiftStatusOpen,
		).
		Order("shifts.opened_at DESC").
		First(&current).Error
	if err != nil {
		return nil, openShiftLookupError(err, "no open shift for current device")
	}

	return &current, nil
}

// GetShift backs `GET /shifts/:id`.
func (r *RepositoryImpl) GetShift(ctx context.Context, orgID, id uint) (*Shift, error) {
	var shift Shift
	err := r.db.WithContext(ctx).
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where("shifts.id = ? AND branches.org_id = ?", id, orgID).
		First(&shift).Error
	if err != nil {
		return nil, openShiftLookupError(err, "shift not found")
	}
	return &shift, nil
}

// CloseShift backs `POST /shifts/:id/close`. Writes reconciliation row(s) as a side effect
func (r *RepositoryImpl) CloseShift(ctx context.Context, orgID, id uint, closedAt time.Time) (*Shift, error) {
	var closed Shift
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: clause.CurrentTable}}).
			Joins("JOIN branches ON branches.id = shifts.branch_id").
			Where("shifts.id = ? AND branches.org_id = ?", id, orgID).
			First(&closed).Error
		if err != nil {
			return openShiftLookupError(err, "shift not found")
		}
		if closed.Status != ShiftStatusOpen {
			return common.ConflictError("shift is already closed")
		}

		var openSales int64
		if err := tx.Model(&sales.Sale{}).
			Where("shift_id = ? AND status = ?", closed.ID, sales.SaleStatusOpen).
			Count(&openSales).Error; err != nil {
			return err
		}
		if openSales > 0 {
			return common.ConflictError("shift has open sales")
		}

		type paymentTotal struct {
			Method sales.PaymentMethod
			Total  decimal.Decimal
		}
		var paymentTotals []paymentTotal
		if err := tx.Model(&sales.Payment{}).
			Select("payments.method, COALESCE(SUM(payments.amount), 0) AS total").
			Joins("JOIN sales ON sales.id = payments.sale_id").
			Where("sales.shift_id = ? AND sales.status = ?", closed.ID, sales.SaleStatusCompleted).
			Group("payments.method").
			Scan(&paymentTotals).Error; err != nil {
			return err
		}

		expectedByMethod := map[ReconciliationMethod]decimal.Decimal{
			ReconciliationMethodCash: closed.OpeningCash,
		}
		for _, payment := range paymentTotals {
			method := reconciliationMethodForPayment(payment.Method)
			expectedByMethod[method] = expectedByMethod[method].Add(payment.Total)
		}

		orderedMethods := []ReconciliationMethod{
			ReconciliationMethodCash,
			ReconciliationMethodCard,
			ReconciliationMethodQR,
			ReconciliationMethodMobile,
			ReconciliationMethodOther,
		}
		reconciliations := make([]ShiftReconciliation, 0, len(expectedByMethod))
		for _, method := range orderedMethods {
			expected, exists := expectedByMethod[method]
			if !exists {
				continue
			}
			reconciliations = append(reconciliations, ShiftReconciliation{
				ShiftID:    closed.ID,
				Method:     method,
				Expected:   expected,
				Counted:    expected,
				Difference: decimal.Zero,
				Reason:     "",
			})
		}
		if err := tx.Create(&reconciliations).Error; err != nil {
			return err
		}

		if err := tx.Model(&Shift{}).
			Where("id = ? AND status = ?", closed.ID, ShiftStatusOpen).
			Updates(map[string]any{"status": ShiftStatusClosed, "closed_at": closedAt}).Error; err != nil {
			return err
		}
		closed.Status = ShiftStatusClosed
		closed.ClosedAt = &closedAt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &closed, nil
}

func reconciliationMethodForPayment(method sales.PaymentMethod) ReconciliationMethod {
	switch method {
	case sales.PaymentMethodCash:
		return ReconciliationMethodCash
	case sales.PaymentMethodCard:
		return ReconciliationMethodCard
	case sales.PaymentMethodQR:
		return ReconciliationMethodQR
	case sales.PaymentMethodMobileWallet:
		return ReconciliationMethodMobile
	default:
		return ReconciliationMethodOther
	}
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
