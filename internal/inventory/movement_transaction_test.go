//go:build cgo

package inventory

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/identity"
)

type failingLedgerStore struct {
	MovementStore
	failure error
}

func (s failingLedgerStore) CreateInventoryLedgerEntry(_ *gorm.DB, _ *InventoryLedger) error {
	return s.failure
}

func movementTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&StockLevel{}, &InventoryLedger{}, &StockAdjustment{}))
	return db
}

func TestApplyMovement_LedgerFailureRollsBackStockAndCallerWrites(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "create"
		if existing {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			db := movementTestDB(t)
			if existing {
				require.NoError(t, db.Create(&StockLevel{ProductID: 9, BranchID: 5, Qty: 7}).Error)
			}
			failure := errors.New("ledger unavailable")
			store := failingLedgerStore{MovementStore: NewRepository(db), failure: failure}
			err := db.Transaction(func(tx *gorm.DB) error {
				adjustment := StockAdjustment{ProductID: 9, BranchID: 5, Delta: 2, ActorID: 30, Reason: "receipt"}
				if err := tx.Create(&adjustment).Error; err != nil {
					return err
				}
				_, err := ApplyMovement(tx, store, InventoryLedger{OrgID: 7, ProductID: 9, BranchID: 5, Qty: 2}, RejectNegativeStock)
				return err
			})
			require.ErrorIs(t, err, failure)
			var adjustments int64
			require.NoError(t, db.Model(&StockAdjustment{}).Count(&adjustments).Error)
			require.Zero(t, adjustments)
			var levels []StockLevel
			require.NoError(t, db.Find(&levels).Error)
			if existing {
				require.Len(t, levels, 1)
				require.Equal(t, 7, levels[0].Qty)
			} else {
				require.Empty(t, levels)
			}
		})
	}
}

func TestStockAdjustment_InsufficientStockRollsBackAdjustmentRecord(t *testing.T) {
	db := movementTestDB(t)
	require.NoError(t, db.Create(&StockLevel{ProductID: 9, BranchID: 5, Qty: 1}).Error)
	branches := NewMockBranchLookup(t)
	products := NewMockProductLookup(t)
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5}, nil).Once()
	products.EXPECT().GetProduct(mock.Anything, uint(7), uint(9)).Return(&catalog.Product{ID: 9}, nil).Once()
	svc := NewService(NewRepository(db), branches, products, db)
	_, err := svc.CreateStockAdjustment(context.Background(), 7, 30, CreateStockAdjustmentRequest{BranchID: 5, ProductID: 9, Delta: -2, Reason: "damage"})
	requireRestErrorStatus(t, err, http.StatusConflict)
	var count int64
	require.NoError(t, db.Model(&StockAdjustment{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&InventoryLedger{}).Count(&count).Error)
	require.Zero(t, count)
	var stock StockLevel
	require.NoError(t, db.First(&stock).Error)
	require.Equal(t, 1, stock.Qty)
}
