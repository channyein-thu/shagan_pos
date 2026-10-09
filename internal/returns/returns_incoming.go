package returns

import (
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/sales"
)

type incomingItem struct {
	SaleItemID uint
	Qty        int
	Condition  ItemCondition
}

type incomingHistory struct {
	Returned  int
	Exchanged int
}

type evaluatedIncomingItem struct {
	incomingItem
	ProductID uint
	UnitPrice decimal.Decimal
	Value     decimal.Decimal
	Restocked bool
}

type incomingPlan struct {
	Items            []evaluatedIncomingItem
	Total            decimal.Decimal
	RequestedQty     map[uint]int
	ReturnedBefore   map[uint]int
	CreditByProduct  map[uint]int
	WriteoffProducts []uint
}

// planIncomingItems reads each item's history once while the caller holds the
// sale lock. All valuation, entitlement and stock-disposition rules live in
// the pure evaluator below; no writes happen here.
func (s *Service) planIncomingItems(tx *gorm.DB, saleItems []sales.SaleItem, items []incomingItem) (*incomingPlan, error) {
	byID := make(map[uint]sales.SaleItem, len(saleItems))
	for _, item := range saleItems {
		byID[item.ID] = item
	}
	history := make(map[uint]incomingHistory, len(items))
	for _, item := range items {
		if _, ok := byID[item.SaleItemID]; !ok {
			return nil, common.BadRequestError("sale_item_id does not belong to this sale")
		}
		if _, loaded := history[item.SaleItemID]; loaded {
			continue
		}
		returned, err := s.repo.ReturnedQtyForSaleItem(tx, item.SaleItemID)
		if err != nil {
			return nil, err
		}
		exchanged, err := s.repo.ExchangedInQtyForSaleItem(tx, item.SaleItemID)
		if err != nil {
			return nil, err
		}
		history[item.SaleItemID] = incomingHistory{Returned: returned, Exchanged: exchanged}
	}
	return evaluateIncomingItems(byID, history, items)
}

// evaluateIncomingItems uses one rounding policy for returns and exchanges:
// credit the change in cumulative proportional value, rounded to cents.
// This allocates fractional cents consistently across repeated request lines
// and later requests. Operations using this policy credit the original total
// when all units are returned. Historic amounts are not rewritten or corrected;
// history provides quantities only.
func evaluateIncomingItems(byID map[uint]sales.SaleItem, history map[uint]incomingHistory, items []incomingItem) (*incomingPlan, error) {
	plan := &incomingPlan{
		Items:           make([]evaluatedIncomingItem, 0, len(items)),
		Total:           decimal.Zero,
		RequestedQty:    make(map[uint]int),
		ReturnedBefore:  make(map[uint]int),
		CreditByProduct: make(map[uint]int),
	}
	for _, item := range items {
		original, ok := byID[item.SaleItemID]
		if !ok {
			return nil, common.BadRequestError("sale_item_id does not belong to this sale")
		}
		if item.Qty <= 0 || original.Qty <= 0 {
			return nil, common.BadRequestError("incoming and original item quantities must be positive")
		}
		prior := history[item.SaleItemID]
		used := prior.Returned + prior.Exchanged + plan.RequestedQty[item.SaleItemID]
		if item.Qty > original.Qty-used {
			return nil, common.BadRequestError("incoming qty exceeds what remains returnable for this item")
		}
		qty := decimal.NewFromInt(int64(original.Qty))
		valueAt := func(units int) decimal.Decimal {
			return original.LineTotal.Mul(decimal.NewFromInt(int64(units))).DivRound(qty, 2)
		}
		value := valueAt(used + item.Qty).Sub(valueAt(used))
		restocked := item.Condition == ItemConditionSellable
		plan.Items = append(plan.Items, evaluatedIncomingItem{
			incomingItem: item, ProductID: original.ProductID,
			UnitPrice: original.LineTotal.DivRound(qty, 2), Value: value, Restocked: restocked,
		})
		plan.Total = plan.Total.Add(value)
		plan.RequestedQty[item.SaleItemID] += item.Qty
		plan.ReturnedBefore[item.SaleItemID] = prior.Returned
		if restocked {
			plan.CreditByProduct[original.ProductID] += item.Qty
		} else {
			plan.WriteoffProducts = append(plan.WriteoffProducts, original.ProductID)
		}
	}
	return plan, nil
}
