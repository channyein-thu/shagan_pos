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
