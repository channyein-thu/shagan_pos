//go:build cgo

package sales

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newSalesRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Sale{}))
	return db
}

func seedSaleBy(t *testing.T, db *gorm.DB, orgID, staffID uint, ago time.Duration) Sale {
	t.Helper()
	completed := time.Now().UTC().Add(-ago)
	sale := Sale{
		ID: uuid.New(), OrgID: orgID, BranchID: 1, ShiftID: 1, StaffID: staffID, DeviceID: 1,
		Total: decimal.NewFromInt(10), Status: SaleStatusCompleted, CompletedAt: &completed,
	}
	require.NoError(t, db.Create(&sale).Error)
	return sale
}

func TestRepository_ListSales_StaffFilterReturnsOnlyThatStaffsSalesAndCountsThemOnly(t *testing.T) {
	db := newSalesRepoTestDB(t)
	mine1 := seedSaleBy(t, db, 7, 14, time.Hour)
	mine2 := seedSaleBy(t, db, 7, 14, 2*time.Hour)
	seedSaleBy(t, db, 7, 15, 3*time.Hour)
	seedSaleBy(t, db, 8, 14, 4*time.Hour) // another org, same staff id
	staff := uint(14)

	got, total, err := NewRepository(db).ListSales(context.Background(), 7, SaleFilter{StaffID: &staff, Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, got, 2)
	require.Equal(t, mine1.ID, got[0].ID, "newest first")
	require.Equal(t, mine2.ID, got[1].ID)
}

func TestRepository_ListSales_NoStaffFilterReturnsEveryoneInTheOrg(t *testing.T) {
	db := newSalesRepoTestDB(t)
	seedSaleBy(t, db, 7, 14, time.Hour)
	seedSaleBy(t, db, 7, 15, 2*time.Hour)
	seedSaleBy(t, db, 8, 14, 3*time.Hour)

	got, total, err := NewRepository(db).ListSales(context.Background(), 7, SaleFilter{Page: 1, PageSize: 20})

	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, got, 2)
}

func newApprovalRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newSalesRepoTestDB(t)
	require.NoError(t, db.AutoMigrate(&DiscountApproval{}))
	return db
}

func seedApproval(t *testing.T, db *gorm.DB, orgID uint, status DiscountApprovalStatus) DiscountApproval {
	t.Helper()
	a := DiscountApproval{
		ID: uuid.New(), OrgID: orgID, BranchID: 1, ShiftID: 1, RequestedBy: 14, ApprovedBy: 17, ProductID: 2,
		Percent: decimal.NewFromInt(10), MaxQty: 2, Status: status, ExpiresAt: time.Now().Add(time.Hour),
	}
	require.NoError(t, db.Create(&a).Error)
	return a
}

// "Once" must hold when two sales race for the same approval: both read it as
// approved, but only one conditional UPDATE can still find it that way.
func TestRepository_MarkDiscountApprovalUsed_OnlyTheFirstSaleWins(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	a := seedApproval(t, db, 7, DiscountApprovalApproved)
	repo := NewRepository(db)
	first, second := uuid.New(), uuid.New()

	won, err := repo.MarkDiscountApprovalUsed(db, a.ID, first)
	require.NoError(t, err)
	require.True(t, won)
	won, err = repo.MarkDiscountApprovalUsed(db, a.ID, second)
	require.NoError(t, err)
	require.False(t, won, "already spent")

	got, err := repo.GetDiscountApproval(context.Background(), 7, a.ID)
	require.NoError(t, err)
	require.Equal(t, DiscountApprovalUsed, got.Status)
	require.NotNil(t, got.SaleID)
	require.Equal(t, first, *got.SaleID)
}

func TestRepository_MarkDiscountApprovalUsed_CancelledApprovalCannotBeSpent(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	a := seedApproval(t, db, 7, DiscountApprovalCancelled)

	won, err := NewRepository(db).MarkDiscountApprovalUsed(db, a.ID, uuid.New())

	require.NoError(t, err)
	require.False(t, won)
}

func TestRepository_CancelDiscountApproval_OnlyWhileStillApproved(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	repo := NewRepository(db)
	open := seedApproval(t, db, 7, DiscountApprovalApproved)
	spent := seedApproval(t, db, 7, DiscountApprovalUsed)

	cancelled, err := repo.CancelDiscountApproval(db, open.ID)
	require.NoError(t, err)
	require.True(t, cancelled)
	cancelled, err = repo.CancelDiscountApproval(db, spent.ID)
	require.NoError(t, err)
	require.False(t, cancelled, "a used approval stays used")
	got, err := repo.GetDiscountApproval(context.Background(), 7, spent.ID)
	require.NoError(t, err)
	require.Equal(t, DiscountApprovalUsed, got.Status)
}

func TestRepository_GetDiscountApproval_AnotherOrgsApprovalIsNotFound(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	a := seedApproval(t, db, 8, DiscountApprovalApproved)

	_, err := NewRepository(db).GetDiscountApproval(context.Background(), 7, a.ID)

	require.Error(t, err)
	require.True(t, isNotFound(err))
}

func TestRepository_ListDiscountApprovals_ScopedToTheOrgAndTheRequestedIDs(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	mine := seedApproval(t, db, 7, DiscountApprovalApproved)
	seedApproval(t, db, 7, DiscountApprovalApproved) // not asked for
	theirs := seedApproval(t, db, 8, DiscountApprovalApproved)

	got, err := NewRepository(db).ListDiscountApprovals(context.Background(), 7, []uuid.UUID{mine.ID, theirs.ID, uuid.New()})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, mine.ID, got[0].ID)
}

func TestRepository_ListDiscountApprovals_NoIDsIsEmptyWithoutQuerying(t *testing.T) {
	got, err := NewRepository(nil).ListDiscountApprovals(context.Background(), 7, nil)

	require.NoError(t, err)
	require.Empty(t, got)
}

func tillTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, ddl := range []string{
		`CREATE TABLE users (id integer primary key, org_id integer, account_type text, branch_id integer, device_id integer)`,
		`CREATE TABLE branches (id integer primary key, org_id integer)`,
		`CREATE TABLE shifts (id integer primary key, branch_id integer, device_id integer, status text, opened_at datetime)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}
}

func TestRepository_FindOpenShift_ResolvesTheTillsOwnOpenShift(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	tillTables(t, db)
	require.NoError(t, db.Exec(`INSERT INTO branches VALUES (5, 7)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users VALUES (70, 7, 'pos', 5, 9), (71, 7, 'pos', 5, 10), (72, 7, 'owner', NULL, NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO shifts VALUES
		(1, 5, 9, 'closed', '2026-10-01 08:00:00'),
		(2, 5, 9, 'open',   '2026-10-02 08:00:00'),
		(3, 5, 10, 'open',  '2026-10-02 09:00:00')`).Error)
	repo := NewRepository(db)

	got, err := repo.FindOpenShift(context.Background(), 7, 70)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint(2), got.ID, "this till's device, not the other register's")
	require.Equal(t, uint(5), got.BranchID)

	other, err := repo.FindOpenShift(context.Background(), 7, 71)
	require.NoError(t, err)
	require.Equal(t, uint(3), other.ID)
}

func TestRepository_FindOpenShift_NilWhenThereIsNone(t *testing.T) {
	db := newApprovalRepoTestDB(t)
	tillTables(t, db)
	require.NoError(t, db.Exec(`INSERT INTO branches VALUES (5, 7)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO users VALUES (70, 7, 'pos', 5, 9), (72, 7, 'owner', NULL, NULL)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO shifts VALUES (1, 5, 9, 'closed', '2026-10-01 08:00:00')`).Error)
	repo := NewRepository(db)

	closed, err := repo.FindOpenShift(context.Background(), 7, 70)
	require.NoError(t, err)
	require.Nil(t, closed, "its only shift is closed")
	owner, err := repo.FindOpenShift(context.Background(), 7, 72)
	require.NoError(t, err)
	require.Nil(t, owner, "an owner account has no till")
	otherOrg, err := repo.FindOpenShift(context.Background(), 99, 70)
	require.NoError(t, err)
	require.Nil(t, otherOrg, "another org can't resolve this till")
}
