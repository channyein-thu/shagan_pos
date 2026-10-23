package common

import "github.com/shopspring/decimal"

// kyatStep is the grid computed prices snap to: whole K 100.
var kyatStep = decimal.NewFromInt(100)

// RoundUpKyat100 is how every discounted unit price is rounded: first
// truncate to 2 decimals (the third decimal is never allowed to push a price
// up), then round UP to the next multiple of K 100. An exact multiple stays.
//
//	6,162.50 -> 6,200    6,100.05 -> 6,200    6,100.005 -> 6,100    9,000 -> 9,000
//
// Rounding up (never down) is deliberate: a discount can only ever give the
// customer a price at or below the original once the caller caps it at the
// original, while a price that rounds down would quietly lose the shop money.
func RoundUpKyat100(d decimal.Decimal) decimal.Decimal {
	return d.Truncate(2).Div(kyatStep).Ceil().Mul(kyatStep)
}

// DiscountedUnitPrice is the unit price after a percent discount: the percent
// comes off the ORIGINAL price, the result is rounded with RoundUpKyat100, and
// it is capped at the original - a small price whose rounded-up discount would
// land above itself (K 150 at 10% -> 135 -> 200) simply isn't discounted. A
// percent of 0 (or less) returns the original untouched, never re-rounded.
// Used for the catalog % on a product and for a manual % on a sale line, so
// the two can never be priced differently.
func DiscountedUnitPrice(original, percent decimal.Decimal) decimal.Decimal {
	if !percent.IsPositive() {
		return original
	}
	hundred := decimal.NewFromInt(100)
	discounted := RoundUpKyat100(original.Mul(hundred.Sub(percent)).Div(hundred))
	if discounted.GreaterThan(original) {
		return original
	}
	return discounted
}
