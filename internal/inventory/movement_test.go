package inventory

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestApplyMovement_PoliciesAndLedger(t *testing.T) {
	for _, tc := range []struct {
		name           string
		exists         bool
		current, delta int
		policy         NegativeStockPolicy
		conflict       bool
	}{
		{"first receipt", false, 0, 4, RejectNegativeStock, false},
		{"sale", true, 4, -3, RejectNegativeStock, false},
		{"insufficient", true, 1, -3, RejectNegativeStock, true},
		{"offline sale", true, 1, -3, AllowNegativeStock, false},
		{"offline sale without row", false, 0, -3, AllowNegativeStock, false},
		{"partial replenishment", true, -5, 2, AllowNegativeStock, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMockRepository(t)
			tx := &gorm.DB{}
			var level *StockLevel
			if tc.exists {
				level = &StockLevel{ID: 4, Qty: tc.current}
			}
			store.EXPECT().GetStockLevel(tx, uint(9), uint(5)).Return(level, nil).Once()
			want := tc.current + tc.delta
			actor := uint(30)
			entry := InventoryLedger{OrgID: 7, ProductID: 9, BranchID: 5, Type: LedgerEntryTypeSale, Qty: tc.delta,
				BalanceAfter: 999, ActorID: &actor, ReferenceType: ReferenceTypeSale, ReferenceID: "sale-id"}
			if !tc.conflict {
				if tc.exists {
					store.EXPECT().UpdateStockLevelQty(tx, uint(4), want).Return(nil).Once()
				} else {
					store.EXPECT().CreateStockLevel(tx, mock.MatchedBy(func(l *StockLevel) bool { return l.ProductID == 9 && l.BranchID == 5 && l.Qty == want })).Return(nil).Once()
				}
				store.EXPECT().CreateInventoryLedgerEntry(tx, mock.MatchedBy(func(e *InventoryLedger) bool {
					return e.OrgID == entry.OrgID && e.ProductID == entry.ProductID && e.BranchID == entry.BranchID &&
						e.Type == entry.Type && e.Qty == entry.Qty && e.BalanceAfter == want && e.ActorID == entry.ActorID &&
						e.ReferenceType == entry.ReferenceType && e.ReferenceID == entry.ReferenceID
				})).Return(nil).Once()
			}
			got, err := ApplyMovement(tx, store, entry, tc.policy)
			if tc.conflict {
				requireRestErrorStatus(t, err, http.StatusConflict)
			} else {
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		})
	}
}

func TestApplyMovement_PropagatesPersistenceFailures(t *testing.T) {
	for _, stage := range []string{"read", "create", "update", "ledger"} {
		t.Run(stage, func(t *testing.T) {
			store := NewMockRepository(t)
			failure := errors.New(stage + " failed")
			level := &StockLevel{ID: 4, Qty: 5}
			var readErr error
			if stage == "read" {
				readErr = failure
			}
			if stage == "create" {
				level = nil
			}
			store.EXPECT().GetStockLevel(mock.Anything, uint(9), uint(5)).Return(level, readErr).Once()
			if stage == "create" {
				store.EXPECT().CreateStockLevel(mock.Anything, mock.Anything).Return(failure).Once()
			}
			if stage == "update" {
				store.EXPECT().UpdateStockLevelQty(mock.Anything, uint(4), 7).Return(failure).Once()
			}
			if stage == "ledger" {
				store.EXPECT().UpdateStockLevelQty(mock.Anything, uint(4), 7).Return(nil).Once()
				store.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(failure).Once()
			}
			_, err := ApplyMovement(nil, store, InventoryLedger{ProductID: 9, BranchID: 5, Qty: 2}, RejectNegativeStock)
			require.ErrorIs(t, err, failure)
		})
	}
}

func TestWeightedAverageCost(t *testing.T) {
	require.True(t, d("2").Equal(WeightedAverageCost(10, d("1"), 10, d("3"))))
	require.True(t, d("1.67").Equal(WeightedAverageCost(1, d("1"), 2, d("2"))))
	for _, qty := range []int{0, -5} {
		require.True(t, d("3").Equal(WeightedAverageCost(qty, d("1"), 2, d("3"))))
	}
}
