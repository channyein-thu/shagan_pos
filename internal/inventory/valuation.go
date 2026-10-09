package inventory

import "github.com/shopspring/decimal"

// WeightedAverageCost blends a positive receipt into the branch's existing
// on-hand cost basis. A nonpositive balance has no value to blend. This keeps
// the existing branch-local valuation policy used by receipts and adjustments.
func WeightedAverageCost(currentQty int, currentCost decimal.Decimal, receivedQty int, receivedUnitCost decimal.Decimal) decimal.Decimal {
	if currentQty <= 0 {
		return receivedUnitCost
	}
	existingValue := currentCost.Mul(decimal.NewFromInt(int64(currentQty)))
	receivedValue := receivedUnitCost.Mul(decimal.NewFromInt(int64(receivedQty)))
	totalQty := decimal.NewFromInt(int64(currentQty + receivedQty))
	return existingValue.Add(receivedValue).Div(totalQty).Round(2)
}
