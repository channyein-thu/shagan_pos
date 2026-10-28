//go:build cgo

package shift

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"shagan_pos/internal/identity"
	"shagan_pos/internal/returns"
	"shagan_pos/internal/sales"
)

// shiftWorld is an org (7) with two branches, each with a till and a cashier.
type shiftWorld struct {
	db               *gorm.DB
	repo             Repository
	a, b             *identity.Branch
	staffA, staffB   *identity.Staff
	deviceA, deviceB *identity.Device
	managerA         *identity.Staff
	base             time.Time
}

func newShiftWorld(t *testing.T) *shiftWorld {
	t.Helper()
	db := newOpenShiftTestDB(t)
	a, staffA, deviceA := seedActiveOpenShiftResources(t, db, 7)
	b, staffB, deviceB := seedActiveOpenShiftResources(t, db, 7)
	// name them so the list's names are checkable
	require.NoError(t, db.Model(a).Update("name", "Main Street").Error)
	require.NoError(t, db.Model(b).Update("name", "Mandalay").Error)
	require.NoError(t, db.Model(staffA).Update("name", "Ma Thida").Error)
	require.NoError(t, db.Model(staffB).Update("name", "Ko Min").Error)
	require.NoError(t, db.Model(deviceA).Update("name", "Counter 1").Error)
	require.NoError(t, db.Model(deviceB).Update("name", "Counter 2").Error)
	manager := &identity.Staff{BranchID: a.ID, Name: "Ko Zaw", RoleID: 2, PinHash: "h", Phone: "1", Status: identity.StaffStatusActive}
	require.NoError(t, db.Create(manager).Error)
	return &shiftWorld{
		db: db, repo: NewRepository(db), a: a, b: b, staffA: staffA, staffB: staffB, deviceA: deviceA, deviceB: deviceB,
		managerA: manager, base: time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC),
	}
}

// shiftSpec describes one shift to seed.
type shiftSpec struct {
	branch   *identity.Branch
	staff    *identity.Staff
	device   *identity.Device
	openedAt time.Time
	closedBy *identity.Staff // nil: still open
	// cash reconciliation, for a closed shift
	expected, counted string
	reason            string
}

func (w *shiftWorld) seed(t *testing.T, sp shiftSpec) *Shift {
	t.Helper()
	sh := &Shift{BranchID: sp.branch.ID, StaffID: sp.staff.ID, DeviceID: sp.device.ID, OpenedAt: sp.openedAt, OpeningCash: decimal.NewFromInt(100), Status: ShiftStatusOpen}
	if sp.closedBy != nil {
		closed := sp.openedAt.Add(8 * time.Hour)
		sh.Status, sh.ClosedAt, sh.ClosedByStaffID = ShiftStatusClosed, &closed, &sp.closedBy.ID
	}
	require.NoError(t, w.db.Create(sh).Error)
	if sp.closedBy != nil {
		expected, counted := decimal.RequireFromString(sp.expected), decimal.RequireFromString(sp.counted)
		require.NoError(t, w.db.Create(&[]ShiftReconciliation{
			{ShiftID: sh.ID, Method: ReconciliationMethodCash, Expected: expected, Counted: counted, Difference: counted.Sub(expected), Reason: sp.reason},
			{ShiftID: sh.ID, Method: ReconciliationMethodQR, Expected: decimal.NewFromInt(25), Counted: decimal.NewFromInt(25), Difference: decimal.Zero},
		}).Error)
	}
	return sh
}

func (w *shiftWorld) list(t *testing.T, scope AccessScope, f ShiftFilter) ([]ShiftRow, int64) {
	t.Helper()
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 25
	}
	rows, total, err := w.repo.ListShifts(context.Background(), scope, f)
	require.NoError(t, err)
	return rows, total
}

func ids(rows []ShiftRow) []uint {
	out := make([]uint, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func (w *shiftWorld) day(n int) time.Time { return w.base.Add(time.Duration(n) * 24 * time.Hour) }

// ---- order, paging, total ----

func TestRepository_ListShifts_NewestFirst_Paged_WithATotal(t *testing.T) {
	w := newShiftWorld(t)
	var made []*Shift
	for i := 0; i < 5; i++ {
		made = append(made, w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(i), closedBy: w.staffA, expected: "100", counted: "100"}))
	}
	scope := AccessScope{OrgID: 7}

	all, total := w.list(t, scope, ShiftFilter{})
	require.Equal(t, int64(5), total)
	require.Equal(t, []uint{made[4].ID, made[3].ID, made[2].ID, made[1].ID, made[0].ID}, ids(all), "opened_at descending")

	page1, total := w.list(t, scope, ShiftFilter{Page: 1, PageSize: 2})
	page2, _ := w.list(t, scope, ShiftFilter{Page: 2, PageSize: 2})
	page3, _ := w.list(t, scope, ShiftFilter{Page: 3, PageSize: 2})
	page4, _ := w.list(t, scope, ShiftFilter{Page: 4, PageSize: 2})
	require.Equal(t, int64(5), total, "total_count is the whole match, not the page")
	require.Equal(t, []uint{made[4].ID, made[3].ID}, ids(page1))
	require.Equal(t, []uint{made[2].ID, made[1].ID}, ids(page2))
	require.Equal(t, []uint{made[0].ID}, ids(page3))
	require.Empty(t, page4)
}

func TestRepository_ListShifts_SameOpenedAt_BreaksTiesByIdSoPagesNeverRepeat(t *testing.T) {
	w := newShiftWorld(t)
	at := w.day(0)
	first := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: at, closedBy: w.staffA, expected: "1", counted: "1"})
	second := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: at, closedBy: w.staffB, expected: "1", counted: "1"})

	p1, _ := w.list(t, AccessScope{OrgID: 7}, ShiftFilter{Page: 1, PageSize: 1})
	p2, _ := w.list(t, AccessScope{OrgID: 7}, ShiftFilter{Page: 2, PageSize: 1})

	require.Equal(t, []uint{second.ID}, ids(p1))
	require.Equal(t, []uint{first.ID}, ids(p2))
}

// ---- names, closer, reconciliation ----

func TestRepository_ListShifts_CarriesNamesCloserAndTheCashReconciliation(t *testing.T) {
	w := newShiftWorld(t)
	forced := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(1), closedBy: w.managerA,
		expected: "351000", counted: "349500", reason: "Gave wrong change"})
	open := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(2)})

	rows, _ := w.list(t, AccessScope{OrgID: 7}, ShiftFilter{})

	require.Equal(t, []uint{open.ID, forced.ID}, ids(rows))
	f := rows[1]
	require.Equal(t, "Main Street", f.BranchName)
	require.Equal(t, "Counter 1", f.DeviceName)
	require.Equal(t, "Ma Thida", f.StaffName)
	require.Equal(t, "Ko Zaw", *f.ClosedByName)
	require.Equal(t, w.managerA.ID, *f.ClosedByStaffID)
	require.NotNil(t, f.Cash)
	require.Equal(t, ReconciliationMethodCash, f.Cash.Method, "the CASH row, not the QR one")
	require.True(t, decimal.NewFromInt(-1500).Equal(f.Cash.Difference))
	require.Equal(t, "Gave wrong change", f.Cash.Reason)
	o := rows[0]
	require.Equal(t, "Mandalay", o.BranchName)
	require.Equal(t, "Ko Min", o.StaffName)
	require.Nil(t, o.Cash)
	require.Nil(t, o.ClosedByName)
}

// ---- filters ----

func TestRepository_ListShifts_Filters(t *testing.T) {
	w := newShiftWorld(t)
	// day0 A cashier closed even; day1 B cashier closed short; day2 A manager-closed over;
	// day3 B cashier open; day4 A cashier closed short
	s0 := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0), closedBy: w.staffA, expected: "500", counted: "500"})
	s1 := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(1), closedBy: w.staffB, expected: "500", counted: "450", reason: "short"})
	s2 := w.seed(t, shiftSpec{branch: w.a, staff: w.managerA, device: w.deviceA, openedAt: w.day(2), closedBy: w.managerA, expected: "500", counted: "520", reason: "over"})
	s3 := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(3)})
	s4 := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(4), closedBy: w.staffA, expected: "500", counted: "499", reason: "short"})
	scope := AccessScope{OrgID: 7}
	open, closed := ShiftStatusOpen, ShiftStatusClosed

	cases := map[string]struct {
		f    ShiftFilter
		want []uint
	}{
		"branch":            {ShiftFilter{BranchID: &w.b.ID}, []uint{s3.ID, s1.ID}},
		"status open":       {ShiftFilter{Status: &open}, []uint{s3.ID}},
		"status closed":     {ShiftFilter{Status: &closed}, []uint{s4.ID, s2.ID, s1.ID, s0.ID}},
		"staff":             {ShiftFilter{StaffID: &w.staffB.ID}, []uint{s3.ID, s1.ID}},
		"device":            {ShiftFilter{DeviceID: &w.deviceA.ID}, []uint{s4.ID, s2.ID, s0.ID}},
		"discrepancy short": {ShiftFilter{Discrepancy: DiscrepancyShort}, []uint{s4.ID, s1.ID}},
		"discrepancy over":  {ShiftFilter{Discrepancy: DiscrepancyOver}, []uint{s2.ID}},
		"discrepancy any":   {ShiftFilter{Discrepancy: DiscrepancyAny}, []uint{s4.ID, s2.ID, s1.ID}},
		"short AND branch":  {ShiftFilter{Discrepancy: DiscrepancyShort, BranchID: &w.a.ID}, []uint{s4.ID}},
		"nothing matches":   {ShiftFilter{StaffID: &w.staffA.ID, Status: &open}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rows, total := w.list(t, scope, c.f)
			require.Equal(t, c.want, nonNil(ids(rows)), "rows")
			require.Equal(t, int64(len(c.want)), total, "total_count follows the filter")
		})
	}
}

func nonNil(s []uint) []uint {
	if len(s) == 0 {
		return nil
	}
	return s
}

// The date bounds are instants: opened_at in [start, end).
func TestRepository_ListShifts_OpenedAtRange_StartInclusiveEndExclusive(t *testing.T) {
	w := newShiftWorld(t)
	before := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0).Add(-time.Second), closedBy: w.staffA, expected: "1", counted: "1"})
	atStart := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0), closedBy: w.staffA, expected: "1", counted: "1"})
	inside := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(0).Add(23 * time.Hour), closedBy: w.staffB, expected: "1", counted: "1"})
	atEnd := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(1), closedBy: w.staffB, expected: "1", counted: "1"})
	start, end := w.day(0), w.day(1)

	rows, total := w.list(t, AccessScope{OrgID: 7}, ShiftFilter{Start: &start, End: &end})

	require.Equal(t, []uint{inside.ID, atStart.ID}, ids(rows))
	require.Equal(t, int64(2), total)
	_ = before
	_ = atEnd
}

// ---- scope ----

func TestRepository_ListShifts_IsConfinedToTheOrgAndTheBranchBoundScope(t *testing.T) {
	w := newShiftWorld(t)
	mine := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0), closedBy: w.staffA, expected: "1", counted: "1"})
	theirs := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(1), closedBy: w.staffB, expected: "1", counted: "1"})
	// another organization's branch and shift
	otherBranch, otherStaff, otherDevice := seedActiveOpenShiftResources(t, w.db, 99)
	foreign := w.seed(t, shiftSpec{branch: otherBranch, staff: otherStaff, device: otherDevice, openedAt: w.day(2), closedBy: otherStaff, expected: "1", counted: "1"})

	owner, _ := w.list(t, AccessScope{OrgID: 7}, ShiftFilter{})
	require.Equal(t, []uint{theirs.ID, mine.ID}, ids(owner), "an Owner sees every branch of the org - and nothing of another org")

	bound, total := w.list(t, AccessScope{OrgID: 7, BranchID: &w.a.ID}, ShiftFilter{})
	require.Equal(t, []uint{mine.ID}, ids(bound), "a branch-bound caller sees only their branch")
	require.Equal(t, int64(1), total)

	// naming another branch while bound to one never widens it
	other, _ := w.list(t, AccessScope{OrgID: 7, BranchID: &w.a.ID}, ShiftFilter{BranchID: &w.b.ID})
	require.Empty(t, other)

	// naming another org's branch finds nothing
	cross, _ := w.list(t, AccessScope{OrgID: 7}, ShiftFilter{BranchID: &otherBranch.ID})
	require.Empty(t, cross)
	_ = foreign
}

// ---- the figures are the summary's figures ----

// seedActivity gives a shift sales, payments, a voided sale, a cash refund and
// cash exchanges in both directions - everything the expected cash is made of.
func (w *shiftWorld) seedActivity(t *testing.T, sh *Shift) {
	t.Helper()
	done := time.Now().UTC()
	mk := func(total string, status sales.SaleStatus) sales.Sale {
		return sales.Sale{ID: uuid.New(), OrgID: 7, BranchID: sh.BranchID, ShiftID: sh.ID, StaffID: sh.StaffID, DeviceID: sh.DeviceID,
			Total: decimal.RequireFromString(total), Status: status, CompletedAt: &done}
	}
	s1, s2, s3, voided := mk("75", sales.SaleStatusCompleted), mk("40", sales.SaleStatusCompleted), mk("30", sales.SaleStatusRefunded), mk("999", sales.SaleStatusVoided)
	require.NoError(t, w.db.Create(&[]sales.Sale{s1, s2, s3, voided}).Error)
	require.NoError(t, w.db.Create(&[]sales.Payment{
		{SaleID: s1.ID, Method: sales.PaymentMethodCash, Amount: decimal.NewFromInt(50)},
		{SaleID: s1.ID, Method: sales.PaymentMethodQR, Amount: decimal.NewFromInt(25)},
		{SaleID: s2.ID, Method: sales.PaymentMethodCash, Amount: decimal.NewFromInt(40)},
		{SaleID: s3.ID, Method: sales.PaymentMethodCash, Amount: decimal.NewFromInt(30)},
		{SaleID: voided.ID, Method: sales.PaymentMethodCash, Amount: decimal.NewFromInt(999)},
	}).Error)
	seedShiftReturn(t, w.db, &sh.ID, returns.RefundMethodCash, "30")
	seedShiftReturn(t, w.db, &sh.ID, returns.RefundMethodQR, "5") // QR never touches the drawer
	cash := returns.ExchangeMethodCash
	seedShiftExchange(t, w.db, &sh.ID, &cash, "12.50")
	seedShiftExchange(t, w.db, &sh.ID, &cash, "-4")
}

// The list and GET /shifts/:id/summary can never disagree: same figures, for an
// open shift and a closed one.
func TestRepository_ListShifts_FiguresEqualTheShiftSummary_ForOpenAndClosedShifts(t *testing.T) {
	w := newShiftWorld(t)
	closed := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0), closedBy: w.staffA, expected: "1", counted: "1"})
	open := w.seed(t, shiftSpec{branch: w.a, staff: w.staffB, device: w.deviceA, openedAt: w.day(1)})
	quiet := w.seed(t, shiftSpec{branch: w.b, staff: w.staffB, device: w.deviceB, openedAt: w.day(2)})
	w.seedActivity(t, closed)
	w.seedActivity(t, open)
	scope := AccessScope{OrgID: 7}

	rows, _ := w.list(t, scope, ShiftFilter{})
	require.Len(t, rows, 3)
	for _, row := range rows {
		summary, err := w.repo.GetShiftSummary(context.Background(), scope, row.ID)
		require.NoError(t, err)
		require.Equal(t, summary["sales_count"], row.Figures.SalesCount, "sales_count of shift %d", row.ID)
		require.True(t, summary["sales_total"].(decimal.Decimal).Equal(row.Figures.SalesTotal), "sales_total of shift %d", row.ID)
		require.True(t, summary["expected_cash"].(decimal.Decimal).Equal(row.Figures.ExpectedCash(row.OpeningCash)), "expected_cash of shift %d", row.ID)
		require.True(t, summary["cash_refunds"].(decimal.Decimal).Equal(row.Figures.CashRefunds))
		require.True(t, summary["exchange_cash_in"].(decimal.Decimal).Equal(row.Figures.ExchangeCashIn))
		require.True(t, summary["exchange_cash_out"].(decimal.Decimal).Equal(row.Figures.ExchangeCashOut))
		sumPayments := summary["payment_totals"].(map[string]decimal.Decimal)
		require.Len(t, row.Figures.PaymentTotals, len(sumPayments), "payment_totals of shift %d", row.ID)
		for method, total := range sumPayments {
			require.True(t, total.Equal(row.Figures.PaymentTotals[method]), "%s total of shift %d", method, row.ID)
		}
	}

	// and the figures are the right ones, worked out by hand:
	// completed 75 + 40 + refunded 30 = 3 sales / 145; the voided one doesn't count
	var activity ShiftRow
	for _, row := range rows {
		if row.ID == closed.ID {
			activity = row
		}
	}
	require.Equal(t, int64(3), activity.Figures.SalesCount)
	require.True(t, decimal.NewFromInt(145).Equal(activity.Figures.SalesTotal))
	require.True(t, decimal.NewFromInt(120).Equal(activity.Figures.PaymentTotals["cash"]), "50 + 40 + 30")
	require.True(t, decimal.NewFromInt(25).Equal(activity.Figures.PaymentTotals["qr"]))
	// opening 100 + cash 120 + exchange in 12.50 - exchange out 4 - cash refund 30
	require.True(t, decimal.RequireFromString("198.50").Equal(activity.Figures.ExpectedCash(activity.OpeningCash)))

	var idle ShiftRow
	for _, row := range rows {
		if row.ID == quiet.ID {
			idle = row
		}
	}
	require.Zero(t, idle.Figures.SalesCount)
	require.NotNil(t, idle.Figures.PaymentTotals)
	require.True(t, decimal.NewFromInt(100).Equal(idle.Figures.ExpectedCash(idle.OpeningCash)), "just the opening float")
}

// One query per table for the whole page - never one set per shift.
func TestRepository_ListShifts_QueryCountDoesNotGrowWithThePage(t *testing.T) {
	w := newShiftWorld(t)
	queries := 0
	require.NoError(t, w.db.Callback().Query().Before("gorm:query").Register("count_shift_queries", func(*gorm.DB) { queries++ }))
	require.NoError(t, w.db.Callback().Raw().Before("gorm:raw").Register("count_shift_raw", func(*gorm.DB) { queries++ }))
	countFor := func(n int) int {
		for i := 0; i < n; i++ {
			sh := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(100 + i), closedBy: w.staffA, expected: "1", counted: "1"})
			w.seedActivity(t, sh)
		}
		queries = 0
		_, _ = w.list(t, AccessScope{OrgID: 7}, ShiftFilter{PageSize: 50})
		return queries
	}

	one := countFor(1)
	many := countFor(8)

	require.Positive(t, one)
	require.Equal(t, one, many, "9 shifts on the page cost exactly the queries 1 did")
}

// ---- the shift_id filters on drawer events and expenses ----

func TestRepository_DrawerEventsAndExpenses_CanBeFilteredToOneShift(t *testing.T) {
	w := newShiftWorld(t)
	s1 := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0)})
	s2 := w.seed(t, shiftSpec{branch: w.a, staff: w.managerA, device: w.deviceA, openedAt: w.day(1)})
	for _, sh := range []*Shift{s1, s2, s2} {
		require.NoError(t, w.db.Create(&DrawerEvent{ShiftID: sh.ID, StaffID: sh.StaffID, Reason: "change"}).Error)
	}
	exp := func(shiftID *uint, amount string) {
		require.NoError(t, w.db.Create(&Expense{BranchID: w.a.ID, Date: time.Now(), Category: "supplies", Amount: decimal.RequireFromString(amount), CreatedBy: &w.staffA.ID, ShiftID: shiftID}).Error)
	}
	exp(&s1.ID, "5")
	exp(&s2.ID, "6")
	exp(&s2.ID, "7")
	exp(nil, "8") // logged by the Owner / with no open shift
	scope := AccessScope{OrgID: 7}

	allEvents, err := w.repo.ListDrawerEvents(context.Background(), scope, nil)
	require.NoError(t, err)
	only2, err := w.repo.ListDrawerEvents(context.Background(), scope, &s2.ID)
	require.NoError(t, err)
	allExp, err := w.repo.ListExpenses(context.Background(), scope, nil)
	require.NoError(t, err)
	exp2, err := w.repo.ListExpenses(context.Background(), scope, &s2.ID)
	require.NoError(t, err)

	require.Len(t, allEvents, 3, "no shift_id: everything, as before")
	require.Len(t, only2, 2)
	for _, e := range only2 {
		require.Equal(t, s2.ID, e.ShiftID)
	}
	require.Len(t, allExp, 4)
	require.Len(t, exp2, 2)
	for _, e := range exp2 {
		require.Equal(t, s2.ID, *e.ShiftID)
	}

	// the scope still applies: another org's caller sees nothing of the shift
	foreign, err := w.repo.ListDrawerEvents(context.Background(), AccessScope{OrgID: 99}, &s2.ID)
	require.NoError(t, err)
	require.Empty(t, foreign)
	foreignExp, err := w.repo.ListExpenses(context.Background(), AccessScope{OrgID: 99}, &s2.ID)
	require.NoError(t, err)
	require.Empty(t, foreignExp)
}

// An expense logged at the till is stamped with the shift that staff member has
// open; an Owner's, or one with no open shift, has none.
func TestRepository_CreateExpense_IsStampedWithTheLoggersOpenShift(t *testing.T) {
	w := newShiftWorld(t)
	open := w.seed(t, shiftSpec{branch: w.a, staff: w.staffA, device: w.deviceA, openedAt: w.day(0)})
	closedEarlier := w.seed(t, shiftSpec{branch: w.a, staff: w.managerA, device: w.deviceA, openedAt: w.day(0), closedBy: w.managerA, expected: "1", counted: "1"})
	scope := AccessScope{OrgID: 7}
	create := func(req CreateExpenseRequest) *Expense {
		req.BranchID, req.Date, req.Category, req.Amount = w.a.ID, time.Now(), "supplies", decimal.NewFromInt(5)
		e, err := w.repo.CreateExpense(context.Background(), scope, req)
		require.NoError(t, err)
		return e
	}

	atTill := create(CreateExpenseRequest{CreatedBy: w.staffA.ID})
	noOpenShift := create(CreateExpenseRequest{CreatedBy: w.managerA.ID})
	byOwner := create(CreateExpenseRequest{CreatedByUserID: 5})

	require.NotNil(t, atTill.ShiftID)
	require.Equal(t, open.ID, *atTill.ShiftID)
	require.Nil(t, noOpenShift.ShiftID, "their shift is closed: not stamped with it")
	require.Nil(t, byOwner.ShiftID)
	_ = closedEarlier
}
