//go:build cgo

package reports

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

	"shagan_pos/internal/identity"
	"shagan_pos/internal/sales"
)

func newReportsRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sales.Sale{}, &sales.SaleItem{}, &sales.DiscountApproval{}, &identity.Staff{}))
	return db
}

var (
	periodStart = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	periodEnd   = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	inPeriod    = time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
)

func seedStaffNamed(t *testing.T, db *gorm.DB, id uint, name string) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO staffs (id, branch_id, name, role, pin_hash, phone, status, failed_pin_attempts, created_at, updated_at)
		VALUES (?, 1, ?, 1, 'x', '1', 'active', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, id, name).Error)
}

type lineSpec struct {
	catalog, manual string
	approver        uint // 0 = no manual discount
}

func seedSaleWith(t *testing.T, db *gorm.DB, orgID, branchID, seller uint, status sales.SaleStatus, at time.Time, lines ...lineSpec) {
	t.Helper()
	sale := sales.Sale{
		ID: uuid.New(), OrgID: orgID, BranchID: branchID, ShiftID: 1, StaffID: seller, DeviceID: 1,
		Total: decimal.NewFromInt(100), Status: status, CompletedAt: &at,
	}
	require.NoError(t, db.Create(&sale).Error)
	for _, l := range lines {
		item := sales.SaleItem{
			SaleID: sale.ID, ProductID: 1, NameSnapshot: "x", Qty: 1, UnitPrice: decimal.NewFromInt(100),
			LineTotal: decimal.NewFromInt(100), CatalogDiscount: d(l.catalog), ManualDiscount: d(l.manual),
		}
		if l.approver != 0 {
			id, approver := uuid.New(), l.approver
			item.DiscountApprovalID, item.ManualApprovedBy = &id, &approver
		}
		require.NoError(t, db.Create(&item).Error)
	}
}

func TestRepository_DiscountsReport_SplitsCatalogFromManualAndNamesWhoAskedAndWhoApproved(t *testing.T) {
	db := newReportsRepoTestDB(t)
	seedStaffNamed(t, db, 14, "Ma Thandar")
	seedStaffNamed(t, db, 15, "Ko Zin")
	seedStaffNamed(t, db, 17, "Ko Zaw")
	completed := sales.SaleStatusCompleted
	seedSaleWith(t, db, 7, 1, 14, completed, inPeriod, lineSpec{"600", "0", 0}, lineSpec{"0", "300", 17})
	seedSaleWith(t, db, 7, 1, 14, completed, inPeriod, lineSpec{"0", "200", 17})
	seedSaleWith(t, db, 7, 1, 15, completed, inPeriod, lineSpec{"100", "0", 0}, lineSpec{"0", "1000", 15})
	seedSaleWith(t, db, 7, 1, 15, sales.SaleStatusRefunded, inPeriod, lineSpec{"0", "50", 17}) // refunded sales still count

	got, err := NewRepository(db).DiscountsReport(context.Background(), 7, nil, periodStart, periodEnd)

	require.NoError(t, err)
	require.True(t, d("700").Equal(got.CatalogTotal))
	require.True(t, d("1550").Equal(got.ManualTotal))
	require.Equal(t, 4, got.ManualCount)
	require.Len(t, got.BySeller, 2)
	require.Equal(t, DiscountStaffRow{StaffID: 15, Name: "Ko Zin", ManualTotal: got.BySeller[0].ManualTotal, Count: 2}, got.BySeller[0])
	require.True(t, d("1050").Equal(got.BySeller[0].ManualTotal))
	require.Equal(t, uint(14), got.BySeller[1].StaffID)
	require.Equal(t, "Ma Thandar", got.BySeller[1].Name)
	require.True(t, d("500").Equal(got.BySeller[1].ManualTotal))
	require.Equal(t, 2, got.BySeller[1].Count)
	require.Len(t, got.ByApprover, 2)
	require.Equal(t, uint(15), got.ByApprover[0].StaffID, "staff 15 approved their own 1,000")
	require.Equal(t, uint(17), got.ByApprover[1].StaffID)
	require.Equal(t, "Ko Zaw", got.ByApprover[1].Name)
	require.True(t, d("550").Equal(got.ByApprover[1].ManualTotal))
	require.Equal(t, 3, got.ByApprover[1].Count)
}

func TestRepository_DiscountsReport_LeavesOutVoidedOpenOutOfWindowOtherOrgAndOtherBranchSales(t *testing.T) {
	db := newReportsRepoTestDB(t)
	completed := sales.SaleStatusCompleted
	seedSaleWith(t, db, 7, 1, 14, completed, inPeriod, lineSpec{"100", "0", 0})
	seedSaleWith(t, db, 7, 1, 14, sales.SaleStatusVoided, inPeriod, lineSpec{"900", "0", 0})
	seedSaleWith(t, db, 7, 1, 14, sales.SaleStatusOpen, inPeriod, lineSpec{"900", "0", 0})
	seedSaleWith(t, db, 7, 1, 14, completed, periodStart.Add(-time.Hour), lineSpec{"900", "0", 0})
	seedSaleWith(t, db, 7, 1, 14, completed, periodEnd, lineSpec{"900", "0", 0}) // the upper bound is exclusive
	seedSaleWith(t, db, 8, 1, 14, completed, inPeriod, lineSpec{"900", "0", 0})
	seedSaleWith(t, db, 7, 2, 14, completed, inPeriod, lineSpec{"20", "0", 0})
	repo := NewRepository(db)
	branch := uint(1)

	all, err := repo.DiscountsReport(context.Background(), 7, nil, periodStart, periodEnd)
	require.NoError(t, err)
	require.True(t, d("120").Equal(all.CatalogTotal), "both of the org's branches")

	one, err := repo.DiscountsReport(context.Background(), 7, &branch, periodStart, periodEnd)
	require.NoError(t, err)
	require.True(t, d("100").Equal(one.CatalogTotal), "branch 1 only")
}

// A combo line's share, and a legacy line with only the old combined discount,
// are neither catalog nor manual: they aren't in these totals.
func TestRepository_DiscountsReport_IgnoresLinesWithOnlyAComboOrLegacyDiscount(t *testing.T) {
	db := newReportsRepoTestDB(t)
	seedSaleWith(t, db, 7, 1, 14, sales.SaleStatusCompleted, inPeriod, lineSpec{"0", "0", 0})
	require.NoError(t, db.Model(&sales.SaleItem{}).Where("1 = 1").Updates(map[string]any{"combo_discount": 40, "discount": 40}).Error)

	got, err := NewRepository(db).DiscountsReport(context.Background(), 7, nil, periodStart, periodEnd)

	require.NoError(t, err)
	require.True(t, got.CatalogTotal.IsZero())
	require.True(t, got.ManualTotal.IsZero())
	require.Equal(t, 0, got.ManualCount)
}

func TestRepository_DiscountsReport_EmptyPeriod_IsAllZeros(t *testing.T) {
	db := newReportsRepoTestDB(t)

	got, err := NewRepository(db).DiscountsReport(context.Background(), 7, nil, periodStart, periodEnd)

	require.NoError(t, err)
	require.True(t, got.CatalogTotal.IsZero())
	require.True(t, got.ManualTotal.IsZero())
	require.Equal(t, 0, got.ManualCount)
	require.Equal(t, 0, got.CancelledCount)
	require.Empty(t, got.BySeller)
	require.Empty(t, got.ByApprover)
}

func TestRepository_DiscountsReport_CountsCancelledApprovalsInTheWindowAndBranch(t *testing.T) {
	db := newReportsRepoTestDB(t)
	mk := func(org, branch uint, status sales.DiscountApprovalStatus, at time.Time) {
		a := sales.DiscountApproval{
			ID: uuid.New(), OrgID: org, BranchID: branch, ShiftID: 1, RequestedBy: 14, ApprovedBy: 17, ProductID: 1,
			Percent: decimal.NewFromInt(10), MaxQty: 1, Status: status, ExpiresAt: at.Add(time.Hour), CreatedAt: at,
		}
		require.NoError(t, db.Create(&a).Error)
	}
	mk(7, 1, sales.DiscountApprovalCancelled, inPeriod)
	mk(7, 1, sales.DiscountApprovalCancelled, inPeriod)
	mk(7, 2, sales.DiscountApprovalCancelled, inPeriod)
	mk(7, 1, sales.DiscountApprovalUsed, inPeriod)                         // spent: not cancelled
	mk(7, 1, sales.DiscountApprovalApproved, inPeriod)                     // still open
	mk(7, 1, sales.DiscountApprovalCancelled, periodStart.Add(-time.Hour)) // before the window
	mk(8, 1, sales.DiscountApprovalCancelled, inPeriod)                    // another org
	repo := NewRepository(db)
	branch := uint(1)

	all, err := repo.DiscountsReport(context.Background(), 7, nil, periodStart, periodEnd)
	require.NoError(t, err)
	require.Equal(t, 3, all.CancelledCount)
	one, err := repo.DiscountsReport(context.Background(), 7, &branch, periodStart, periodEnd)
	require.NoError(t, err)
	require.Equal(t, 2, one.CancelledCount)
}
