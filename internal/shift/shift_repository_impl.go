package shift

import (
	"context"
	"errors"
	"strings"
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

// CloseShift backs `POST /shifts/:id/close`. Writes reconciliation row(s) as
// a side effect. Only the shift's own staff member may call this - see
// closeShift.
func (r *RepositoryImpl) CloseShift(ctx context.Context, scope AccessScope, id uint, closedAt time.Time, staffID uint, in CloseShiftRequest) (*Shift, error) {
	return r.closeShift(ctx, scope, id, closedAt, staffID, &staffID, in)
}

// ForceCloseShift backs `POST /shifts/:id/force-close`. Same reconciliation
// logic as CloseShift, but skips the "same staff who opened it" check - see
// closeShift.
func (r *RepositoryImpl) ForceCloseShift(ctx context.Context, scope AccessScope, id uint, closedAt time.Time, closedByStaffID uint, in CloseShiftRequest) (*Shift, error) {
	return r.closeShift(ctx, scope, id, closedAt, closedByStaffID, nil, in)
}

// closeShift is the shared transaction behind CloseShift/ForceCloseShift.
// requireOpenerStaffID, when non-nil, must match the shift's own StaffID -
// omitted entirely for a force-close, whose whole point is bypassing that
// check. closedByStaffID is always recorded, whichever path was taken.
func (r *RepositoryImpl) closeShift(ctx context.Context, scope AccessScope, id uint, closedAt time.Time, closedByStaffID uint, requireOpenerStaffID *uint, in CloseShiftRequest) (*Shift, error) {
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
		if requireOpenerStaffID != nil && closed.StaffID != *requireOpenerStaffID {
			return common.ForbiddenError("only the staff member who opened this shift may close it")
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
			Where("sales.shift_id = ? AND sales.status IN ?", closed.ID, settledSaleStatuses).
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
		adjustments, err := shiftCashAdjustmentsFor(tx, closed.ID)
		if err != nil {
			return err
		}
		expectedByMethod[ReconciliationMethodCash] = expectedByMethod[ReconciliationMethodCash].Add(adjustments.net())

		orderedMethods := []ReconciliationMethod{
			ReconciliationMethodCash,
			ReconciliationMethodQR,
		}
		// Only cash is physically counted at close - QR payments settle
		// electronically, so they're trusted to match what was recorded
		// (Counted == Expected, Difference always zero). Cash uses what was
		// actually counted in the drawer; a non-zero difference requires a
		// reason, since that's the one number that can genuinely be short or
		// over.
		expectedCash := expectedByMethod[ReconciliationMethodCash]
		cashDifference := in.ClosingCash.Sub(expectedCash)
		if !cashDifference.IsZero() && strings.TrimSpace(in.Reason) == "" {
			return common.BadRequestError("reason is required when closing_cash does not match the expected cash total")
		}

		reconciliations := make([]ShiftReconciliation, 0, len(expectedByMethod))
		for _, method := range orderedMethods {
			expected, exists := expectedByMethod[method]
			if !exists {
				continue
			}
			counted := expected
			difference := decimal.Zero
			reason := ""
			if method == ReconciliationMethodCash {
				counted = in.ClosingCash
				difference = cashDifference
				reason = strings.TrimSpace(in.Reason)
			}
			reconciliations = append(reconciliations, ShiftReconciliation{
				ShiftID:    closed.ID,
				Method:     method,
				Expected:   expected,
				Counted:    counted,
				Difference: difference,
				Reason:     reason,
			})
		}
		if err := tx.Create(&reconciliations).Error; err != nil {
			return err
		}

		if err := tx.Model(&Shift{}).
			Where("id = ? AND status = ?", closed.ID, ShiftStatusOpen).
			Updates(map[string]any{"status": ShiftStatusClosed, "closed_at": closedAt, "closed_by_staff_id": closedByStaffID}).Error; err != nil {
			return err
		}
		// An approved-but-unspent custom discount only lives as long as the
		// shift it was approved on - in the same transaction as the close, so
		// there is no moment where the shift is closed and an approval is
		// still spendable.
		if err := tx.Model(&sales.DiscountApproval{}).
			Where("shift_id = ? AND status = ?", closed.ID, sales.DiscountApprovalApproved).
			Update("status", sales.DiscountApprovalCancelled).Error; err != nil {
			return err
		}
		closed.Status = ShiftStatusClosed
		closed.ClosedAt = &closedAt
		closed.ClosedByStaffID = &closedByStaffID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &closed, nil
}

// settledSaleStatuses are the sale statuses whose payments reached the till:
// a fully returned ("refunded") sale was still paid for during its shift - its
// cash going back out is the cash refund subtracted separately - so it
// counts exactly like a completed one. Voided and open sales never do.
var settledSaleStatuses = []sales.SaleStatus{sales.SaleStatusCompleted, sales.SaleStatusRefunded}

// cashAdjustments is the cash that refunds and exchanges moved through a
// shift's drawer on top of its sales: Refunds is what cash-method Returns
// handed back, ExchangeIn is the extra cash customers paid on an Exchange,
// ExchangeOut is the cash customers were handed back on one. QR refunds and
// QR exchange differences settle electronically and never appear here.
type cashAdjustments struct {
	Refunds     decimal.Decimal
	ExchangeIn  decimal.Decimal
	ExchangeOut decimal.Decimal
}

// net is the change to the drawer's expected cash.
func (a cashAdjustments) net() decimal.Decimal {
	return a.ExchangeIn.Sub(a.ExchangeOut).Sub(a.Refunds)
}

// shiftCashAdjustmentsFor is cashAdjustmentsByShift for one shift. Shared by
// closeShift (the reconciliation snapshot) and loadShiftFigures (the summary
// and the shift list), so the printed expected cash, the list and the snapshot
// can never disagree.
func shiftCashAdjustmentsFor(db *gorm.DB, shiftID uint) (cashAdjustments, error) {
	all, err := cashAdjustmentsByShift(db, []uint{shiftID})
	if err != nil {
		return cashAdjustments{}, err
	}
	return all[shiftID], nil
}

// cashAdjustmentsByShift sums the cash refunds and cash exchange differences
// stamped with each of shiftIDs, in one grouped query per table - never one per
// shift. It reads the returns domain's tables by name rather than importing it,
// same reason sales.RequireOpenShift does it the other way round - returns
// already depends on sales, and shift on both would cycle. A shift with none
// has the zero cashAdjustments.
func cashAdjustmentsByShift(db *gorm.DB, shiftIDs []uint) (map[uint]cashAdjustments, error) {
	out := make(map[uint]cashAdjustments, len(shiftIDs))
	if len(shiftIDs) == 0 {
		return out, nil
	}
	var refunds []struct {
		ShiftID uint
		Total   decimal.Decimal
	}
	if err := db.Table("returns").
		Select("shift_id, COALESCE(SUM(refund_total), 0) AS total").
		Where("shift_id IN ? AND refund_method = ?", shiftIDs, "cash").
		Group("shift_id").
		Scan(&refunds).Error; err != nil {
		return nil, err
	}
	for _, r := range refunds {
		a := out[r.ShiftID]
		a.Refunds = r.Total
		out[r.ShiftID] = a
	}
	var exchanges []struct {
		ShiftID uint
		CashIn  decimal.Decimal
		CashOut decimal.Decimal
	}
	if err := db.Table("exchanges").
		Select("shift_id, "+
			"COALESCE(SUM(CASE WHEN net_difference > 0 THEN net_difference ELSE 0 END), 0) AS cash_in, "+
			"COALESCE(SUM(CASE WHEN net_difference < 0 THEN -net_difference ELSE 0 END), 0) AS cash_out").
		Where("shift_id IN ? AND method = ?", shiftIDs, "cash").
		Group("shift_id").
		Scan(&exchanges).Error; err != nil {
		return nil, err
	}
	for _, e := range exchanges {
		a := out[e.ShiftID]
		a.ExchangeIn, a.ExchangeOut = e.CashIn, e.CashOut
		out[e.ShiftID] = a
	}
	return out, nil
}

// loadShiftFigures is THE calculation behind a shift's sales count and total,
// per-method payment totals and expected cash - used by GetShiftSummary (one
// id) and by ListShifts (a page of ids), so they cannot differ. Grouped
// queries over shiftIDs: one for sales, one for payments, and one each for cash
// refunds and cash exchanges (see cashAdjustmentsByShift). A shift with no
// activity has the zero ShiftFigures (empty, non-nil PaymentTotals).
func loadShiftFigures(db *gorm.DB, shiftIDs []uint) (map[uint]ShiftFigures, error) {
	out := make(map[uint]ShiftFigures, len(shiftIDs))
	if len(shiftIDs) == 0 {
		return out, nil
	}
	for _, id := range shiftIDs {
		out[id] = ShiftFigures{PaymentTotals: map[string]decimal.Decimal{}}
	}

	var salesTotals []struct {
		ShiftID    uint
		SalesCount int64
		SalesTotal decimal.Decimal
	}
	if err := db.Model(&sales.Sale{}).
		Select("shift_id, COUNT(*) AS sales_count, COALESCE(SUM(total), 0) AS sales_total").
		Where("shift_id IN ? AND status IN ?", shiftIDs, settledSaleStatuses).
		Group("shift_id").
		Scan(&salesTotals).Error; err != nil {
		return nil, err
	}
	for _, t := range salesTotals {
		f := out[t.ShiftID]
		f.SalesCount, f.SalesTotal = t.SalesCount, t.SalesTotal
		out[t.ShiftID] = f
	}

	var methodTotals []struct {
		ShiftID uint
		Method  sales.PaymentMethod
		Total   decimal.Decimal
	}
	if err := db.Model(&sales.Payment{}).
		Select("sales.shift_id AS shift_id, payments.method AS method, COALESCE(SUM(payments.amount), 0) AS total").
		Joins("JOIN sales ON sales.id = payments.sale_id").
		Where("sales.shift_id IN ? AND sales.status IN ?", shiftIDs, settledSaleStatuses).
		Group("sales.shift_id, payments.method").
		Scan(&methodTotals).Error; err != nil {
		return nil, err
	}
	for _, t := range methodTotals {
		out[t.ShiftID].PaymentTotals[string(t.Method)] = t.Total
	}

	adjustments, err := cashAdjustmentsByShift(db, shiftIDs)
	if err != nil {
		return nil, err
	}
	for id, a := range adjustments {
		f := out[id]
		f.CashRefunds, f.ExchangeCashIn, f.ExchangeCashOut = a.Refunds, a.ExchangeIn, a.ExchangeOut
		out[id] = f
	}
	return out, nil
}

func reconciliationMethodForPayment(method sales.PaymentMethod) ReconciliationMethod {
	switch method {
	case sales.PaymentMethodQR:
		return ReconciliationMethodQR
	default:
		return ReconciliationMethodCash
	}
}

// GetShiftSummary backs `GET /shifts/:id/summary`. Printable summary. The
// figures come from loadShiftFigures - the same calculation every `GET /shifts`
// row uses.
func (r *RepositoryImpl) GetShiftSummary(ctx context.Context, scope AccessScope, id uint) (map[string]any, error) {
	db := r.db.WithContext(ctx)
	shift, err := getShiftInScope(db, scope, id, false)
	if err != nil {
		return nil, err
	}
	all, err := loadShiftFigures(db, []uint{shift.ID})
	if err != nil {
		return nil, err
	}
	figures := all[shift.ID]

	reconciliations, err := r.ListShiftReconciliations(ctx, scope, shift.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"shift":             *shift,
		"sales_count":       figures.SalesCount,
		"sales_total":       figures.SalesTotal,
		"payment_totals":    figures.PaymentTotals,
		"expected_cash":     figures.ExpectedCash(shift.OpeningCash),
		"cash_refunds":      figures.CashRefunds,
		"exchange_cash_in":  figures.ExchangeCashIn,
		"exchange_cash_out": figures.ExchangeCashOut,
		"reconciliations":   reconciliations,
	}, nil
}

// ListShifts backs `GET /shifts`: one page of the scope's shifts matching f,
// newest first (opened_at, then id, so pages never repeat or skip), with the
// names behind the ids, each shift's cash reconciliation row and its live
// figures - all gathered with queries over the page's ids, never per shift.
func (r *RepositoryImpl) ListShifts(ctx context.Context, scope AccessScope, f ShiftFilter) ([]ShiftRow, int64, error) {
	db := r.db.WithContext(ctx)
	base := db.Model(&Shift{}).
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where("branches.org_id = ?", scope.OrgID)
	if scope.BranchID != nil {
		base = base.Where("shifts.branch_id = ?", *scope.BranchID)
	}
	if f.BranchID != nil {
		base = base.Where("shifts.branch_id = ?", *f.BranchID)
	}
	if f.Start != nil {
		base = base.Where("shifts.opened_at >= ?", *f.Start)
	}
	if f.End != nil {
		base = base.Where("shifts.opened_at < ?", *f.End)
	}
	if f.Status != nil {
		base = base.Where("shifts.status = ?", *f.Status)
	}
	if f.StaffID != nil {
		base = base.Where("shifts.staff_id = ?", *f.StaffID)
	}
	if f.DeviceID != nil {
		base = base.Where("shifts.device_id = ?", *f.DeviceID)
	}
	if cond, ok := discrepancyCondition(f.Discrepancy); ok {
		// only a closed shift has a counted cash to be short or over
		base = base.Where("shifts.status = ?", ShiftStatusClosed).
			Where("EXISTS (SELECT 1 FROM shifts_reconciliations r WHERE r.shift_id = shifts.id AND r.method = ? AND "+cond+")", ReconciliationMethodCash)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var shifts []Shift
	if err := base.Session(&gorm.Session{}).
		Select("shifts.*").
		Order("shifts.opened_at DESC, shifts.id DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&shifts).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]ShiftRow, len(shifts))
	if len(shifts) == 0 {
		return rows, total, nil
	}

	shiftIDs := make([]uint, len(shifts))
	branchIDs, deviceIDs, staffIDs := []uint{}, []uint{}, []uint{}
	for i, sh := range shifts {
		shiftIDs[i] = sh.ID
		branchIDs = append(branchIDs, sh.BranchID)
		deviceIDs = append(deviceIDs, sh.DeviceID)
		staffIDs = append(staffIDs, sh.StaffID)
		if sh.ClosedByStaffID != nil {
			staffIDs = append(staffIDs, *sh.ClosedByStaffID)
		}
	}
	branchNames, err := namesByID(db, &identity.Branch{}, branchIDs)
	if err != nil {
		return nil, 0, err
	}
	deviceNames, err := namesByID(db, &identity.Device{}, deviceIDs)
	if err != nil {
		return nil, 0, err
	}
	staffNames, err := namesByID(db, &identity.Staff{}, staffIDs)
	if err != nil {
		return nil, 0, err
	}

	var cash []ShiftReconciliation
	if err := db.Where("shift_id IN ? AND method = ?", shiftIDs, ReconciliationMethodCash).Find(&cash).Error; err != nil {
		return nil, 0, err
	}
	cashByShift := make(map[uint]*ShiftReconciliation, len(cash))
	for i := range cash {
		cashByShift[cash[i].ShiftID] = &cash[i]
	}

	figures, err := loadShiftFigures(db, shiftIDs)
	if err != nil {
		return nil, 0, err
	}
	for i, sh := range shifts {
		row := ShiftRow{
			Shift:      sh,
			BranchName: branchNames[sh.BranchID], DeviceName: deviceNames[sh.DeviceID], StaffName: staffNames[sh.StaffID],
			Cash: cashByShift[sh.ID], Figures: figures[sh.ID],
		}
		if sh.ClosedByStaffID != nil {
			name := staffNames[*sh.ClosedByStaffID]
			row.ClosedByName = &name
		}
		rows[i] = row
	}
	return rows, total, nil
}

// discrepancyCondition is the SQL on a cash reconciliation row r for one
// discrepancy filter value (difference = counted - expected).
func discrepancyCondition(d string) (string, bool) {
	switch d {
	case DiscrepancyShort:
		return "r.difference < 0", true
	case DiscrepancyOver:
		return "r.difference > 0", true
	case DiscrepancyAny:
		return "r.difference <> 0", true
	}
	return "", false
}

// namesByID reads the name column of model's rows with the given ids in one
// query. model must have id and name columns (Branch, Device, Staff).
func namesByID(db *gorm.DB, model any, ids []uint) (map[uint]string, error) {
	var rows []struct {
		ID   uint
		Name string
	}
	if err := db.Model(model).Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
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
func (r *RepositoryImpl) ListDrawerEvents(ctx context.Context, scope AccessScope, shiftID *uint) ([]DrawerEvent, error) {
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
	if shiftID != nil {
		query = query.Where("drawer_events.shift_id = ?", *shiftID)
	}
	if err := query.Order("drawer_events.created_at DESC, drawer_events.id DESC").Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// ListExpenses backs `GET /expenses`.
func (r *RepositoryImpl) ListExpenses(ctx context.Context, scope AccessScope, shiftID *uint) ([]Expense, error) {
	var expenses []Expense
	query := r.db.WithContext(ctx).
		Model(&Expense{}).
		Select("expenses.*").
		Joins("JOIN branches ON branches.id = expenses.branch_id").
		Where("branches.org_id = ?", scope.OrgID)
	if scope.BranchID != nil {
		query = query.Where("expenses.branch_id = ?", *scope.BranchID)
	}
	if shiftID != nil {
		query = query.Where("expenses.shift_id = ?", *shiftID)
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
		expense = Expense{
			BranchID: in.BranchID,
			Date:     in.Date,
			Category: in.Category,
			Amount:   in.Amount,
		}
		if in.CreatedBy != 0 {
			if err := requireActiveStaffInBranch(tx, in.CreatedBy, in.BranchID); err != nil {
				return err
			}
			expense.CreatedBy = &in.CreatedBy
			// logged at the till: it belongs to the shift that staff member has open
			var open []Shift
			if err := tx.Where("staff_id = ? AND branch_id = ? AND status = ?", in.CreatedBy, in.BranchID, ShiftStatusOpen).
				Order("id DESC").Limit(1).Find(&open).Error; err != nil {
				return err
			}
			if len(open) > 0 {
				expense.ShiftID = &open[0].ID
			}
		} else {
			expense.CreatedByUserID = &in.CreatedByUserID
		}
		return tx.Create(&expense).Error
	})
	if err != nil {
		return nil, err
	}
	return &expense, nil
}

// UpdateExpense backs `PATCH /expenses/:id`.
func (r *RepositoryImpl) UpdateExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor, in UpdateExpenseRequest) (*Expense, error) {
	var expense Expense
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		found, err := getExpenseInScope(tx, scope, id, true)
		if err != nil {
			return err
		}
		if !actor.CanManageAny && !found.createdByStaff(actor.StaffID) {
			return common.ForbiddenError("only the staff member who logged this expense, or a manager, may modify it")
		}
		expense = *found
		effectiveBranchID := expense.BranchID
		if in.BranchID != nil {
			effectiveBranchID = *in.BranchID
		}
		if err := requireActiveBranchInScope(tx, scope, effectiveBranchID); err != nil {
			return err
		}
		// An expense an Owner logged has no staff creator to re-validate
		// against the (possibly changed) branch; only check when there is one.
		effectiveStaffID := expense.CreatedBy
		if in.CreatedBy != nil {
			effectiveStaffID = in.CreatedBy
		}
		if effectiveStaffID != nil {
			if err := requireActiveStaffInBranch(tx, *effectiveStaffID, effectiveBranchID); err != nil {
				return err
			}
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
			// Reassigned to a staff member: no longer an Owner-logged expense.
			updates["created_by"] = *in.CreatedBy
			updates["created_by_user_id"] = nil
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
func (r *RepositoryImpl) DeleteExpense(ctx context.Context, scope AccessScope, id uint, actor ExpenseActor) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		expense, err := getExpenseInScope(tx, scope, id, true)
		if err != nil {
			return err
		}
		if !actor.CanManageAny && !expense.createdByStaff(actor.StaffID) {
			return common.ForbiddenError("only the staff member who logged this expense, or a manager, may delete it")
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
