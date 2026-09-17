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
func (r *RepositoryImpl) GetShift(ctx context.Context, scope AccessScope, id uint) (*Shift, error) {
	return getShiftInScope(r.db.WithContext(ctx), scope, id, false)
}

// CloseShift backs `POST /shifts/:id/close`. Writes reconciliation row(s) as a side effect
func (r *RepositoryImpl) CloseShift(ctx context.Context, scope AccessScope, id uint, closedAt time.Time) (*Shift, error) {
	var closed Shift
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		found, err := getShiftInScope(tx, scope, id, true)
		if err != nil {
			return err
		}
		closed = *found
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
func (r *RepositoryImpl) GetShiftSummary(ctx context.Context, scope AccessScope, id uint) (map[string]any, error) {
	db := r.db.WithContext(ctx)
	shift, err := getShiftInScope(db, scope, id, false)
	if err != nil {
		return nil, err
	}

	type salesSummary struct {
		SalesCount int64
		SalesTotal decimal.Decimal
	}
	var salesTotals salesSummary
	if err := db.Model(&sales.Sale{}).
		Select("COUNT(*) AS sales_count, COALESCE(SUM(total), 0) AS sales_total").
		Where("shift_id = ? AND status = ?", shift.ID, sales.SaleStatusCompleted).
		Scan(&salesTotals).Error; err != nil {
		return nil, err
	}

	type methodTotal struct {
		Method sales.PaymentMethod
		Total  decimal.Decimal
	}
	var methodTotals []methodTotal
	if err := db.Model(&sales.Payment{}).
		Select("payments.method, COALESCE(SUM(payments.amount), 0) AS total").
		Joins("JOIN sales ON sales.id = payments.sale_id").
		Where("sales.shift_id = ? AND sales.status = ?", shift.ID, sales.SaleStatusCompleted).
		Group("payments.method").
		Scan(&methodTotals).Error; err != nil {
		return nil, err
	}
	paymentTotals := make(map[string]decimal.Decimal, len(methodTotals))
	expectedCash := shift.OpeningCash
	for _, total := range methodTotals {
		paymentTotals[string(total.Method)] = total.Total
		if total.Method == sales.PaymentMethodCash {
			expectedCash = expectedCash.Add(total.Total)
		}
	}

	reconciliations, err := r.ListShiftReconciliations(ctx, scope, shift.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"shift":           *shift,
		"sales_count":     salesTotals.SalesCount,
		"sales_total":     salesTotals.SalesTotal,
		"payment_totals":  paymentTotals,
		"expected_cash":   expectedCash,
		"reconciliations": reconciliations,
	}, nil
}

// ListShiftReconciliations backs `GET /shifts/:id/reconciliations`. Per-method breakdown
func (r *RepositoryImpl) ListShiftReconciliations(ctx context.Context, scope AccessScope, id uint) ([]ShiftReconciliation, error) {
	db := r.db.WithContext(ctx)
	if _, err := getShiftInScope(db, scope, id, false); err != nil {
		return nil, err
	}
	var reconciliations []ShiftReconciliation
	if err := db.Where("shift_id = ?", id).Order("method ASC").Find(&reconciliations).Error; err != nil {
		return nil, err
	}
	return reconciliations, nil
}

// CreateDrawerEvent backs `POST /drawer-events`. Cash drawer opened without a sale
func (r *RepositoryImpl) CreateDrawerEvent(ctx context.Context, scope AccessScope, in CreateDrawerEventRequest) (*DrawerEvent, error) {
	var event DrawerEvent
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		shift, err := getShiftInScope(tx, scope, in.ShiftID, true)
		if err != nil {
			return err
		}
		if shift.Status != ShiftStatusOpen {
			return common.ConflictError("drawer events require an open shift")
		}
		if err := requireActiveBranchInScope(tx, scope, shift.BranchID); err != nil {
			return err
		}
		if err := requireActiveStaffInBranch(tx, in.StaffID, shift.BranchID); err != nil {
			return err
		}
		event = DrawerEvent{
			ShiftID: in.ShiftID,
			StaffID: in.StaffID,
			Reason:  in.Reason,
			SaleID:  in.SaleID,
		}
		return tx.Create(&event).Error
	})
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// ListDrawerEvents backs `GET /drawer-events`.
func (r *RepositoryImpl) ListDrawerEvents(ctx context.Context, scope AccessScope) ([]DrawerEvent, error) {
	var events []DrawerEvent
	query := r.db.WithContext(ctx).
		Model(&DrawerEvent{}).
		Select("drawer_events.*").
		Joins("JOIN shifts ON shifts.id = drawer_events.shift_id").
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where("branches.org_id = ?", scope.OrgID)
	if scope.BranchID != nil {
		query = query.Where("shifts.branch_id = ?", *scope.BranchID)
	}
	if err := query.Order("drawer_events.created_at DESC, drawer_events.id DESC").Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// ListExpenses backs `GET /expenses`.
func (r *RepositoryImpl) ListExpenses(ctx context.Context, scope AccessScope) ([]Expense, error) {
	var expenses []Expense
	query := r.db.WithContext(ctx).
		Model(&Expense{}).
		Select("expenses.*").
		Joins("JOIN branches ON branches.id = expenses.branch_id").
		Where("branches.org_id = ?", scope.OrgID)
	if scope.BranchID != nil {
		query = query.Where("expenses.branch_id = ?", *scope.BranchID)
	}
	if err := query.Order("expenses.date DESC, expenses.id DESC").Find(&expenses).Error; err != nil {
		return nil, err
	}
	return expenses, nil
}

// CreateExpense backs `POST /expenses`.
func (r *RepositoryImpl) CreateExpense(ctx context.Context, scope AccessScope, in CreateExpenseRequest) (*Expense, error) {
	var expense Expense
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := requireActiveBranchInScope(tx, scope, in.BranchID); err != nil {
			return err
		}
		if err := requireActiveStaffInBranch(tx, in.CreatedBy, in.BranchID); err != nil {
			return err
		}
		expense = Expense{
			BranchID:  in.BranchID,
			Date:      in.Date,
			Category:  in.Category,
			Amount:    in.Amount,
			CreatedBy: in.CreatedBy,
		}
		return tx.Create(&expense).Error
	})
	if err != nil {
		return nil, err
	}
	return &expense, nil
}

// UpdateExpense backs `PATCH /expenses/:id`.
func (r *RepositoryImpl) UpdateExpense(ctx context.Context, scope AccessScope, id uint, in UpdateExpenseRequest) (*Expense, error) {
	var expense Expense
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		found, err := getExpenseInScope(tx, scope, id, true)
		if err != nil {
			return err
		}
		expense = *found
		effectiveBranchID := expense.BranchID
		if in.BranchID != nil {
			effectiveBranchID = *in.BranchID
		}
		if err := requireActiveBranchInScope(tx, scope, effectiveBranchID); err != nil {
			return err
		}
		effectiveStaffID := expense.CreatedBy
		if in.CreatedBy != nil {
			effectiveStaffID = *in.CreatedBy
		}
		if err := requireActiveStaffInBranch(tx, effectiveStaffID, effectiveBranchID); err != nil {
			return err
		}

		updates := map[string]any{}
		if in.BranchID != nil {
			updates["branch_id"] = *in.BranchID
		}
		if in.Date != nil {
			updates["date"] = *in.Date
		}
		if in.Category != nil {
			updates["category"] = *in.Category
		}
		if in.Amount != nil {
			updates["amount"] = *in.Amount
		}
		if in.CreatedBy != nil {
			updates["created_by"] = *in.CreatedBy
		}
		if err := tx.Model(&Expense{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&expense, id).Error
	})
	if err != nil {
		return nil, err
	}
	return &expense, nil
}

// DeleteExpense backs `DELETE /expenses/:id`.
func (r *RepositoryImpl) DeleteExpense(ctx context.Context, scope AccessScope, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		expense, err := getExpenseInScope(tx, scope, id, true)
		if err != nil {
			return err
		}
		return tx.Delete(expense).Error
	})
}

func getShiftInScope(db *gorm.DB, scope AccessScope, id uint, lock bool) (*Shift, error) {
	var shift Shift
	query := db.Model(&Shift{}).
		Select("shifts.*").
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where("shifts.id = ? AND branches.org_id = ?", id, scope.OrgID)
	if scope.BranchID != nil {
		query = query.Where("shifts.branch_id = ?", *scope.BranchID)
	}
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: clause.CurrentTable}})
	}
	if err := query.First(&shift).Error; err != nil {
		return nil, openShiftLookupError(err, "shift not found")
	}
	return &shift, nil
}

func requireActiveBranchInScope(db *gorm.DB, scope AccessScope, branchID uint) error {
	var branch identity.Branch
	query := db.Where("id = ? AND org_id = ?", branchID, scope.OrgID)
	if scope.BranchID != nil {
		query = query.Where("id = ?", *scope.BranchID)
	}
	if err := query.First(&branch).Error; err != nil {
		return openShiftLookupError(err, "branch not found")
	}
	if branch.Status != identity.BranchStatusActive {
		return common.ConflictError("branch is not active")
	}
	return nil
}

func requireActiveStaffInBranch(db *gorm.DB, staffID, branchID uint) error {
	var staff identity.Staff
	if err := db.Where("id = ? AND branch_id = ?", staffID, branchID).First(&staff).Error; err != nil {
		return openShiftLookupError(err, "staff not found in branch")
	}
	if staff.Status != identity.StaffStatusActive {
		return common.ConflictError("staff is not active")
	}
	return nil
}

func getExpenseInScope(db *gorm.DB, scope AccessScope, id uint, lock bool) (*Expense, error) {
	var expense Expense
	query := db.Model(&Expense{}).
		Select("expenses.*").
		Joins("JOIN branches ON branches.id = expenses.branch_id").
		Where("expenses.id = ? AND branches.org_id = ?", id, scope.OrgID)
	if scope.BranchID != nil {
		query = query.Where("expenses.branch_id = ?", *scope.BranchID)
	}
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: clause.CurrentTable}})
	}
	if err := query.First(&expense).Error; err != nil {
		return nil, openShiftLookupError(err, "expense not found")
	}
	return &expense, nil
}
