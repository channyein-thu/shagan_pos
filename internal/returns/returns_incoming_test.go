package returns

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/inventory"
	"shagan_pos/internal/sales"
)

func TestIncomingPlan_QuantityAndRounding(t *testing.T) {
	original := map[uint]sales.SaleItem{1: {ID: 1, ProductID: 9, Qty: 3, LineTotal: d("10")}}
	item := func(qty int) incomingItem {
		return incomingItem{SaleItemID: 1, Qty: qty, Condition: ItemConditionSellable}
	}
	whole, err := evaluateIncomingItems(original, nil, []incomingItem{item(2)})
	require.NoError(t, err)
	require.True(t, d("6.67").Equal(whole.Total))
	split, err := evaluateIncomingItems(original, nil, []incomingItem{item(1), item(1)})
	require.NoError(t, err)
	require.True(t, whole.Total.Equal(split.Total), "splitting lines must not change credit")
	require.True(t, d("3.33").Equal(split.Items[0].Value))
	require.True(t, d("3.34").Equal(split.Items[1].Value))
	require.Equal(t, 2, split.RequestedQty[1])
	require.Equal(t, 2, split.CreditByProduct[9])

	last, err := evaluateIncomingItems(original, map[uint]incomingHistory{1: {Returned: 1, Exchanged: 1}}, []incomingItem{item(1)})
	require.NoError(t, err)
	require.True(t, d("10").Equal(whole.Total.Add(last.Total)))
	require.Equal(t, 1, last.ReturnedBefore[1], "exchanges must not count toward fully returned status")

	_, err = evaluateIncomingItems(original, nil, []incomingItem{item(2), item(2)})
	requireRestErrorStatus(t, err, http.StatusBadRequest)
	_, err = evaluateIncomingItems(original, map[uint]incomingHistory{1: {Returned: 1, Exchanged: 1}}, []incomingItem{item(2)})
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestIncomingPlan_DispositionAndInvalidItems(t *testing.T) {
	original := map[uint]sales.SaleItem{1: {ID: 1, ProductID: 9, Qty: 3, LineTotal: d("10")}}
	plan, err := evaluateIncomingItems(original, nil, []incomingItem{
		{SaleItemID: 1, Qty: 1, Condition: ItemConditionSellable},
		{SaleItemID: 1, Qty: 1, Condition: ItemConditionDamaged},
		{SaleItemID: 1, Qty: 1, Condition: ItemConditionExpired},
	})
	require.NoError(t, err)
	require.Equal(t, map[uint]int{9: 1}, plan.CreditByProduct)
	require.Equal(t, []uint{9, 9}, plan.WriteoffProducts)
	require.True(t, d("10").Equal(plan.Total))
	for _, item := range []incomingItem{{SaleItemID: 99, Qty: 1}, {SaleItemID: 1, Qty: 0}, {SaleItemID: 1, Qty: -1}} {
		_, err := evaluateIncomingItems(original, nil, []incomingItem{item})
		requireRestErrorStatus(t, err, http.StatusBadRequest)
	}
	original[1] = sales.SaleItem{ID: 1, Qty: 0}
	_, err = evaluateIncomingItems(original, nil, []incomingItem{{SaleItemID: 1, Qty: 1}})
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

// Exercise both public workflows, so a helper regression cannot be hidden by
// one workflow accidentally retaining its own valuation/quantity rules.
func TestIncomingWorkflows_ShareCreditAndRejectRepeatedOverReturn(t *testing.T) {
	for _, exchange := range []bool{false, true} {
		for _, overReturn := range []bool{false, true} {
			name := "return"
			if exchange {
				name = "exchange"
			}
			if overReturn {
				name += "/over-return"
			} else {
				name += "/fractional-credit"
			}
			t.Run(name, func(t *testing.T) {
				repo := NewMockRepository(t)
				sr := NewMockSalesReader(t)
				inv := NewMockInventoryWriter(t)
				aud := NewMockAuditWriter(t)
				svc := newTestService(repo, NewMockBranchLookup(t), sr, inv, aud)
				id := uuid.New()
				sr.EXPECT().GetSaleWithLock(mock.Anything, uint(7), id).Return(&sales.Sale{ID: id, BranchID: 5, Status: sales.SaleStatusCompleted}, nil).Once()
				sr.EXPECT().ListSaleItemsTx(mock.Anything, id).Return([]sales.SaleItem{{ID: 1, ProductID: 9, Qty: 3, LineTotal: d("10")}}, nil).Once()
				repo.EXPECT().ReturnedQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
				repo.EXPECT().ExchangedInQtyForSaleItem(mock.Anything, uint(1)).Return(0, nil).Once()
				if !overReturn {
					inv.EXPECT().GetStockLevel(mock.Anything, uint(9), uint(5)).Return(&inventory.StockLevel{ID: 4, Qty: 0}, nil).Once()
					inv.EXPECT().UpdateStockLevelQty(mock.Anything, uint(4), 2).Return(nil).Once()
					inv.EXPECT().CreateInventoryLedgerEntry(mock.Anything, mock.Anything).Return(nil).Once()
					aud.EXPECT().CreateAuditLog(mock.Anything, mock.Anything).Return(nil).Once()
					if exchange {
						repo.EXPECT().CreateExchange(mock.Anything, mock.Anything).Return(nil).Once()
						repo.EXPECT().CreateExchangeItems(mock.Anything, mock.Anything).Return(nil).Once()
					} else {
						repo.EXPECT().CreateReturn(mock.Anything, mock.Anything).Return(nil).Once()
						repo.EXPECT().CreateReturnItems(mock.Anything, mock.Anything).Return(nil).Once()
					}
				}
				qty := 1
				if overReturn {
					qty = 2
				}
				actor := Actor{StaffID: 30, CanApprove: true}
				if exchange {
					itemID := uint(1)
					item := CreateExchangeItemRequest{Direction: DirectionIn, SaleItemID: &itemID, Qty: qty}
					got, err := svc.CreateExchange(context.Background(), 7, actor, CreateExchangeRequest{SaleID: id, Method: ExchangeMethodCash, Items: []CreateExchangeItemRequest{item, item}})
					if overReturn {
						requireRestErrorStatus(t, err, http.StatusBadRequest)
					} else {
						require.NoError(t, err)
						require.True(t, d("-6.67").Equal(got.NetDifference))
					}
				} else {
					item := CreateReturnItemRequest{SaleItemID: 1, Qty: qty, Condition: ItemConditionSellable}
					got, err := svc.CreateReturn(context.Background(), 7, actor, CreateReturnRequest{SaleID: id, RefundMethod: RefundMethodCash, Items: []CreateReturnItemRequest{item, item}})
					if overReturn {
						requireRestErrorStatus(t, err, http.StatusBadRequest)
					} else {
						require.NoError(t, err)
						require.True(t, d("6.67").Equal(got.RefundTotal))
					}
				}
			})
		}
	}
}
