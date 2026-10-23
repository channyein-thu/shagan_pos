package common

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Every row of the Kyat rounding table in the discount spec: truncate to 2
// decimals first (never round the 3rd decimal up), then round UP to the next
// K 100; an exact multiple of 100 stays.
func TestRoundUpKyat100(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"6162.50", "6200"},
		{"6152.50", "6200"},
		{"6142.50", "6200"},
		{"6100.05", "6200"},
		{"6100.005", "6100"}, // the 3rd decimal is truncated away, leaving an exact multiple
		{"6100.00", "6100"},
		{"9000.00", "9000"},
		{"0", "0"},
		{"50", "100"},
		{"0.01", "100"},
		{"100", "100"},
		{"99.999", "100"},    // truncates to 99.99, which rounds up to 100
		{"6100.009", "6100"}, // truncates to 6100.00, an exact multiple, so it stays
	}
	for _, tc := range cases {
		got := RoundUpKyat100(decimal.RequireFromString(tc.raw))
		require.True(t, decimal.RequireFromString(tc.want).Equal(got), "RoundUpKyat100(%s) = %s, want %s", tc.raw, got, tc.want)
	}
}

func TestRoundUpKyat100_NeverRoundsDown(t *testing.T) {
	for _, raw := range []string{"1", "99", "101", "5999.99", "6000.01", "123456.78"} {
		in := decimal.RequireFromString(raw)
		require.False(t, RoundUpKyat100(in).LessThan(in.Truncate(2)), raw)
	}
}

func TestRoundUpKyat100_ResultIsAMultipleOf100(t *testing.T) {
	for _, raw := range []string{"1", "99.99", "6142.50", "6162.50", "123456.78"} {
		got := RoundUpKyat100(decimal.RequireFromString(raw))
		require.True(t, got.Mod(decimal.NewFromInt(100)).IsZero(), "%s -> %s", raw, got)
	}
}

// One formula prices both the catalog % and a manual %: the percent comes off
// the ORIGINAL price, the result snaps up to K 100, and it can never end up
// above the original (a small price rounding up past itself is capped).
func TestDiscountedUnitPrice(t *testing.T) {
	cases := []struct{ price, pct, want string }{
		{"6500", "10", "5900"},   // 5,850 -> 5,900
		{"6500", "0", "6500"},    // no discount: exactly the original, not re-rounded
		{"6750", "0", "6750"},    // ...even if it isn't a multiple of 100
		{"6500", "100", "0"},     // fully free
		{"150", "10", "150"},     // 135 would round up to 200 - capped at the original
		{"6500", "50", "3300"},   // 3,250 -> 3,300
		{"9000", "10", "8100"},   // exact multiple stays
		{"1000", "33.33", "700"}, // 666.7 -> 700
	}
	for _, tc := range cases {
		got := DiscountedUnitPrice(decimal.RequireFromString(tc.price), decimal.RequireFromString(tc.pct))
		require.True(t, decimal.RequireFromString(tc.want).Equal(got), "DiscountedUnitPrice(%s, %s%%) = %s, want %s", tc.price, tc.pct, got, tc.want)
	}
}

func TestDiscountedUnitPrice_NeverAboveTheOriginal(t *testing.T) {
	for _, price := range []string{"1", "99", "150", "6750", "6500.50"} {
		for _, pct := range []string{"0", "0.01", "5", "10", "50", "99.99", "100"} {
			p, c := decimal.RequireFromString(price), decimal.RequireFromString(pct)
			require.False(t, DiscountedUnitPrice(p, c).GreaterThan(p), "%s at %s%%", price, pct)
		}
	}
}

// Prices already contain their tax, so the tax is the part of the amount paid:
// amount x p / (100 + p), to 2 decimals, half up. It is never added to anything.
func TestIncludedTax(t *testing.T) {
	cases := []struct{ amount, pct, want string }{
		{"10500", "5", "500"},        // the spec example: K 10,500 at 5%
		{"10500", "0", "0"},          // 0% product
		{"5900", "5", "280.95"},      // 5900 x 5 / 105 = 280.952...
		{"100", "5", "4.76"},         // 4.7619 rounds down
		{"200", "5", "9.52"},         // 9.5238...
		{"105", "5", "5"},            // exact
		{"1", "5", "0.05"},           // 0.0476 -> 0.05 (half up at 2 dp)
		{"0", "5", "0"},              // free line
		{"1000", "100", "500"},       // 100% means half the price is tax
		{"7777.77", "7.5", "542.64"}, // fractional rate
	}
	for _, tc := range cases {
		got := IncludedTax(decimal.RequireFromString(tc.amount), decimal.RequireFromString(tc.pct))
		require.True(t, decimal.RequireFromString(tc.want).Equal(got), "IncludedTax(%s, %s%%) = %s, want %s", tc.amount, tc.pct, got, tc.want)
	}
}

// A negative or zero rate must never produce tax (and never divide oddly).
func TestIncludedTax_NoRateNoTax(t *testing.T) {
	require.True(t, IncludedTax(decimal.NewFromInt(500), decimal.NewFromInt(-5)).IsZero())
	require.True(t, IncludedTax(decimal.NewFromInt(500), decimal.Zero).IsZero())
}
