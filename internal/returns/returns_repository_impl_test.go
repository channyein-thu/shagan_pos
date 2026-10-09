//go:build cgo

package returns

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name)),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Return{}, &ReturnItem{}, &Exchange{}, &ExchangeItem{}))
	return db
}

func seedReturnOf(t *testing.T, db *gorm.DB, saleID uuid.UUID, refund string, lines ...ReturnItem) {
	t.Helper()
	ret := Return{SaleID: saleID, ReasonCode: ReturnReasonCodeOther, RefundMethod: RefundMethodCash, RefundTotal: decimal.RequireFromString(refund), ApprovedBy: 1}
	require.NoError(t, db.Create(&ret).Error)
	for i := range lines {
		lines[i].ReturnID = ret.ID
		lines[i].Condition = ItemConditionSellable
	}
	require.NoError(t, db.Create(&lines).Error)
}

func seedExchangeOf(t *testing.T, db *gorm.DB, saleID uuid.UUID, lines ...ExchangeItem) {
	t.Helper()
	ex := Exchange{SaleID: saleID, NetDifference: decimal.Zero, ApprovedBy: 1}
	require.NoError(t, db.Create(&ex).Error)
	for i := range lines {
		lines[i].ExchangeID = ex.ID
	}
	require.NoError(t, db.Create(&lines).Error)
}

func uintp(v uint) *uint { return &v }

func TestRepository_ReturnedQtyBySaleItems_NothingReturned_IsEmpty(t *testing.T) {
	db := newRepoTestDB(t)

	got, err := NewRepository(db).ReturnedQtyBySaleItems(context.Background(), []uint{1, 2})

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepository_ReturnedQtyBySaleItems_NoIDs_IsEmptyWithoutQuerying(t *testing.T) {
	got, err := NewRepository(nil).ReturnedQtyBySaleItems(context.Background(), nil)

	require.NoError(t, err)
	require.Empty(t, got)
}

// A line's returnable qty is what the guard in CreateReturn/CreateExchange
// enforces, so this must add up the same two sources it does: return lines
// and exchange "in" lines - and nothing else.
func TestRepository_ReturnedQtyBySaleItems_SumsReturnLinesAndExchangeInLinesPerSaleItem(t *testing.T) {
	db := newRepoTestDB(t)
	sale := uuid.New()
	seedReturnOf(t, db, sale, "10", ReturnItem{SaleItemID: 1, Qty: 1}, ReturnItem{SaleItemID: 2, Qty: 3})
	seedReturnOf(t, db, sale, "10", ReturnItem{SaleItemID: 1, Qty: 2})
	seedExchangeOf(t, db, sale,
		ExchangeItem{Direction: DirectionIn, SaleItemID: uintp(1), Qty: 4},
		ExchangeItem{Direction: DirectionIn, SaleItemID: uintp(3), Qty: 5},
		// An "out" line is a new item going to the customer, never a return.
		ExchangeItem{Direction: DirectionOut, ProductID: uintp(9), Qty: 7},
		ExchangeItem{Direction: DirectionOut, SaleItemID: uintp(1), Qty: 100},
	)
	// A line nobody asked about must not leak into the result.
	seedReturnOf(t, db, sale, "1", ReturnItem{SaleItemID: 99, Qty: 6})

	got, err := NewRepository(db).ReturnedQtyBySaleItems(context.Background(), []uint{1, 2, 3, 4})

	require.NoError(t, err)
	require.Equal(t, map[uint]int{1: 1 + 2 + 4, 2: 3, 3: 5}, got)
}

func TestRepository_ReturnSummaries_NoActivity_IsEmpty(t *testing.T) {
	db := newRepoTestDB(t)

	got, err := NewRepository(db).ReturnSummaries(context.Background(), []uuid.UUID{uuid.New()})

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepository_ReturnSummaries_NoIDs_IsEmptyWithoutQuerying(t *testing.T) {
	got, err := NewRepository(nil).ReturnSummaries(context.Background(), nil)

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestRepository_ReturnSummaries_ReturnsAreFlaggedAndTheirRefundsSummed(t *testing.T) {
	db := newRepoTestDB(t)
	sale, other := uuid.New(), uuid.New()
	seedReturnOf(t, db, sale, "12.50", ReturnItem{SaleItemID: 1, Qty: 1})
	seedReturnOf(t, db, sale, "7.25", ReturnItem{SaleItemID: 2, Qty: 1})
	seedReturnOf(t, db, other, "99", ReturnItem{SaleItemID: 3, Qty: 1})

	got, err := NewRepository(db).ReturnSummaries(context.Background(), []uuid.UUID{sale})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.True(t, got[sale].HasReturn)
	require.False(t, got[sale].HasExchange)
	require.True(t, decimal.RequireFromString("19.75").Equal(got[sale].RefundedTotal))
}

func TestRepository_ReturnSummaries_ExchangeOnlyIsFlaggedWithNoRefund(t *testing.T) {
	db := newRepoTestDB(t)
	sale := uuid.New()
	seedExchangeOf(t, db, sale, ExchangeItem{Direction: DirectionIn, SaleItemID: uintp(1), Qty: 1})

	got, err := NewRepository(db).ReturnSummaries(context.Background(), []uuid.UUID{sale})

	require.NoError(t, err)
	require.False(t, got[sale].HasReturn)
	require.True(t, got[sale].HasExchange)
	require.True(t, got[sale].RefundedTotal.IsZero())
}

func TestRepository_ReturnSummaries_SaleWithBothReturnAndExchange(t *testing.T) {
	db := newRepoTestDB(t)
	sale := uuid.New()
	seedReturnOf(t, db, sale, "5", ReturnItem{SaleItemID: 1, Qty: 1})
	seedExchangeOf(t, db, sale, ExchangeItem{Direction: DirectionIn, SaleItemID: uintp(2), Qty: 1})

	got, err := NewRepository(db).ReturnSummaries(context.Background(), []uuid.UUID{sale})

	require.NoError(t, err)
	require.True(t, got[sale].HasReturn)
	require.True(t, got[sale].HasExchange)
	require.True(t, decimal.NewFromInt(5).Equal(got[sale].RefundedTotal))
}
