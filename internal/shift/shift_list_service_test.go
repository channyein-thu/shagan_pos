package shift

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/identity"
)

// listRepoStub is a Repository whose only working method is ListShifts.
type listRepoStub struct {
	Repository
	rows      []ShiftRow
	total     int64
	gotScope  AccessScope
	gotFilter ShiftFilter
	called    bool
	err       error
}

func (r *listRepoStub) ListShifts(_ context.Context, scope AccessScope, f ShiftFilter) ([]ShiftRow, int64, error) {
	r.called, r.gotScope, r.gotFilter = true, scope, f
	return r.rows, r.total, r.err
}

type orgStub struct {
	tz     string
	called bool
}

func (o *orgStub) GetOrganization(_ context.Context, id uint) (*identity.Organization, error) {
	o.called = true
	return &identity.Organization{ID: id, Timezone: o.tz}, nil
}

func newListService(repo *listRepoStub, tz string) (*Service, *orgStub) {
	orgs := &orgStub{tz: tz}
	return NewService(repo, orgs), orgs
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func uptr(n uint) *uint            { return &n }

// ---- paging and the envelope ----

func TestService_ListShifts_DefaultsAndClampsPaging_AndWrapsTheEnvelope(t *testing.T) {
	cases := []struct {
		page, size         int
		wantPage, wantSize int
	}{
		{0, 0, 1, 25}, {-4, -1, 1, 25}, {3, 10, 3, 10}, {2, 5000, 2, 100},
	}
	for _, c := range cases {
		repo := &listRepoStub{total: 132}
		svc, _ := newListService(repo, "Asia/Yangon")

		got, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{Page: c.page, PageSize: c.size})

		require.NoError(t, err)
		require.Equal(t, c.wantPage, got.Page)
		require.Equal(t, c.wantSize, got.PageSize)
		require.Equal(t, int64(132), got.TotalCount)
		require.NotNil(t, got.Shifts, "never null")
		require.Equal(t, c.wantPage, repo.gotFilter.Page)
		require.Equal(t, c.wantSize, repo.gotFilter.PageSize)
	}
}

// ---- filters ----

func TestService_ListShifts_PassesTheFiltersThrough(t *testing.T) {
	repo := &listRepoStub{}
	svc, orgs := newListService(repo, "Asia/Yangon")

	_, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{
		BranchID: uptr(3), StaffID: uptr(21), DeviceID: uptr(7), Status: "closed", Discrepancy: "short",
	})

	require.NoError(t, err)
	f := repo.gotFilter
	require.Equal(t, uint(3), *f.BranchID)
	require.Equal(t, uint(21), *f.StaffID)
	require.Equal(t, uint(7), *f.DeviceID)
	require.Equal(t, ShiftStatusClosed, *f.Status)
	require.Equal(t, DiscrepancyShort, f.Discrepancy)
	require.False(t, orgs.called, "no date bound, so the org's timezone isn't read")
}

// from / to are calendar days of the ORGANIZATION's zone, matched against
// opened_at: 2026-10-01 .. 2026-10-27 in Yangon (UTC+6:30) is 17:30 UTC the day
// before through 17:30 UTC on the 27th - to is inclusive, its whole day counts.
func TestService_ListShifts_DatesAreDaysOfTheOrganizationsTimezone(t *testing.T) {
	repo := &listRepoStub{}
	svc, orgs := newListService(repo, "Asia/Yangon")
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)

	_, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{From: &from, To: &to})

	require.NoError(t, err)
	require.True(t, orgs.called)
	require.Equal(t, time.Date(2026, 9, 30, 17, 30, 0, 0, time.UTC), repo.gotFilter.Start.UTC())
	require.Equal(t, time.Date(2026, 10, 27, 17, 30, 0, 0, time.UTC), repo.gotFilter.End.UTC(), "the end of the 27th, exclusive")
}

func TestService_ListShifts_ADifferentZoneMovesTheDayBoundaries(t *testing.T) {
	repo := &listRepoStub{}
	svc, _ := newListService(repo, "Asia/Bangkok") // UTC+7
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

	_, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{From: &day, To: &day})

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC), repo.gotFilter.Start.UTC())
	require.Equal(t, time.Date(2026, 10, 5, 17, 0, 0, 0, time.UTC), repo.gotFilter.End.UTC())
}

func TestService_ListShifts_OnlyFrom_LeavesTheEndOpen(t *testing.T) {
	repo := &listRepoStub{}
	svc, _ := newListService(repo, "Asia/Yangon")
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	_, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{From: &from})

	require.NoError(t, err)
	require.NotNil(t, repo.gotFilter.Start)
	require.Nil(t, repo.gotFilter.End)
}

func TestService_ListShifts_BadStatusOrDiscrepancy_IsBadRequestAndNeverReadsAnything(t *testing.T) {
	for name, q := range map[string]ShiftListQuery{
		"status":      {Status: "paused"},
		"discrepancy": {Discrepancy: "wrong"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &listRepoStub{}
			svc, _ := newListService(repo, "Asia/Yangon")

			_, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, q)

			requireRestErrorStatus(t, err, http.StatusBadRequest)
			require.False(t, repo.called)
		})
	}
}

// ---- branch scope ----

func TestService_ListShifts_ManagerAtATill_OtherBranchIs403_OwnBranchIsFine(t *testing.T) {
	own, other := uint(3), uint(4)
	repo := &listRepoStub{}
	svc, _ := newListService(repo, "Asia/Yangon")
	scope := AccessScope{OrgID: 7, BranchID: &own}

	_, err := svc.ListShifts(context.Background(), scope, ShiftListQuery{BranchID: &other})
	requireRestErrorStatus(t, err, http.StatusForbidden)
	require.False(t, repo.called, "refused before anything is read")

	_, err = svc.ListShifts(context.Background(), scope, ShiftListQuery{BranchID: &own})
	require.NoError(t, err)
	_, err = svc.ListShifts(context.Background(), scope, ShiftListQuery{})
	require.NoError(t, err)
	require.Equal(t, scope, repo.gotScope, "the branch-bound scope reaches the repository, which confines the query to it")
}

func TestService_ListShifts_OrgWideScope_MayNameAnyBranch(t *testing.T) {
	repo := &listRepoStub{}
	svc, _ := newListService(repo, "Asia/Yangon")

	_, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{BranchID: uptr(9)})

	require.NoError(t, err)
	require.Equal(t, uint(9), *repo.gotFilter.BranchID)
}

// ---- row shaping ----

func closedRow(staffID uint, closedBy *uint, closedByName *string) ShiftRow {
	opened := time.Date(2026, 10, 27, 2, 0, 0, 0, time.UTC)
	closed := opened.Add(8 * time.Hour)
	return ShiftRow{
		Shift: Shift{ID: 412, BranchID: 3, StaffID: staffID, DeviceID: 7, OpenedAt: opened, ClosedAt: &closed,
			ClosedByStaffID: closedBy, OpeningCash: dec("50000"), Status: ShiftStatusClosed},
		BranchName: "Main Street", DeviceName: "Counter 1", StaffName: "Ma Thida", ClosedByName: closedByName,
		Cash: &ShiftReconciliation{ShiftID: 412, Method: ReconciliationMethodCash, Expected: dec("351000"), Counted: dec("349500"), Difference: dec("-1500"), Reason: "Gave wrong change"},
		Figures: ShiftFigures{
			SalesCount: 38, SalesTotal: dec("412500"),
			PaymentTotals: map[string]decimal.Decimal{"cash": dec("301000"), "qr": dec("111500")},
		},
	}
}

func TestService_ListShifts_AClosedRow_CarriesEveryField(t *testing.T) {
	name := "Ma Thida"
	repo := &listRepoStub{rows: []ShiftRow{closedRow(21, uptr(21), &name)}, total: 1}
	svc, _ := newListService(repo, "Asia/Yangon")

	got, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{})

	require.NoError(t, err)
	row := got.Shifts[0]
	require.Equal(t, uint(412), row.ID)
	require.Equal(t, ShiftStatusClosed, row.Status)
	require.Equal(t, "Main Street", row.BranchName)
	require.Equal(t, "Counter 1", row.DeviceName)
	require.Equal(t, "Ma Thida", row.StaffName)
	require.False(t, row.ForceClosed, "closed by the staff who opened it")
	require.Equal(t, uint(21), *row.ClosedByStaffID)
	require.NotNil(t, row.ClosedAt)
	require.Equal(t, int64(38), row.SalesCount)
	// 50,000 opening + 301,000 cash sales
	require.True(t, dec("351000").Equal(row.ExpectedCash))
	require.True(t, dec("349500").Equal(*row.CountedCash))
	require.True(t, dec("-1500").Equal(*row.CashDifference))
	require.Equal(t, "Gave wrong change", *row.DifferenceReason)
}

// force_closed is true only when someone OTHER than the shift's own staff closed it.
func TestService_ListShifts_ForceClosedOnlyWhenSomeoneElseClosedIt(t *testing.T) {
	zaw := "Ko Zaw"
	self := "Ma Thida"
	repo := &listRepoStub{rows: []ShiftRow{
		closedRow(21, uptr(9), &zaw),   // someone else closed it
		closedRow(21, uptr(21), &self), // the staff closed their own
		closedRow(21, nil, nil),        // a legacy closed shift with no closer recorded
	}}
	svc, _ := newListService(repo, "Asia/Yangon")

	got, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{})

	require.NoError(t, err)
	require.True(t, got.Shifts[0].ForceClosed)
	require.Equal(t, "Ko Zaw", *got.Shifts[0].ClosedByName)
	require.False(t, got.Shifts[1].ForceClosed)
	require.False(t, got.Shifts[2].ForceClosed)
}

// An open shift has live totals, and no counted cash, difference, reason or
// closer yet.
func TestService_ListShifts_AnOpenRow_HasLiveTotalsAndNullClosedFields(t *testing.T) {
	row := ShiftRow{
		Shift:      Shift{ID: 9, BranchID: 3, StaffID: 21, DeviceID: 7, OpenedAt: time.Now(), OpeningCash: dec("100"), Status: ShiftStatusOpen},
		BranchName: "Main Street", DeviceName: "Counter 1", StaffName: "Ma Thida",
		Figures: ShiftFigures{SalesCount: 2, SalesTotal: dec("75"), PaymentTotals: map[string]decimal.Decimal{"cash": dec("50"), "qr": dec("25")},
			CashRefunds: dec("10")},
	}
	svc, _ := newListService(&listRepoStub{rows: []ShiftRow{row}}, "Asia/Yangon")

	got, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{})

	require.NoError(t, err)
	r := got.Shifts[0]
	require.Equal(t, ShiftStatusOpen, r.Status)
	require.True(t, dec("140").Equal(r.ExpectedCash), "100 + 50 cash sales - 10 cash refund")
	require.Equal(t, int64(2), r.SalesCount)
	require.Nil(t, r.ClosedAt)
	require.Nil(t, r.ClosedByStaffID)
	require.Nil(t, r.ClosedByName)
	require.Nil(t, r.CountedCash)
	require.Nil(t, r.CashDifference)
	require.Nil(t, r.DifferenceReason)
	require.False(t, r.ForceClosed)
}

func TestService_ListShifts_ARowWithNoActivity_HasEmptyNotNullPaymentTotals(t *testing.T) {
	row := ShiftRow{Shift: Shift{ID: 9, OpeningCash: dec("100"), Status: ShiftStatusOpen, OpenedAt: time.Now()}}
	svc, _ := newListService(&listRepoStub{rows: []ShiftRow{row}}, "Asia/Yangon")

	got, err := svc.ListShifts(context.Background(), AccessScope{OrgID: 7}, ShiftListQuery{})

	require.NoError(t, err)
	require.NotNil(t, got.Shifts[0].PaymentTotals)
	require.True(t, dec("100").Equal(got.Shifts[0].ExpectedCash))
	require.True(t, got.Shifts[0].SalesTotal.IsZero())
}
