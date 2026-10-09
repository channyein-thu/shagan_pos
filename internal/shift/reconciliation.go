package shift

import (
	"strings"

	"github.com/shopspring/decimal"

	"shagan_pos/internal/common"
	"shagan_pos/internal/sales"
)

// paymentRules is the single classification and display order used by both
// shift closing and summaries. New methods must be explicitly classified.
var paymentRules = []struct {
	Payment         sales.PaymentMethod
	Reconciliation  ReconciliationMethod
	CountedInDrawer bool
}{
	{sales.PaymentMethodCash, ReconciliationMethodCash, true},
	{sales.PaymentMethodQR, ReconciliationMethodQR, false},
}

type paymentTotal struct {
	Method sales.PaymentMethod
	Total  decimal.Decimal
}

// cashAdjustments describes movements beyond original sale payments.
// Electronic refunds/exchange differences do not affect the drawer.
type cashAdjustments struct {
	Refunds     decimal.Decimal
	ExchangeIn  decimal.Decimal
	ExchangeOut decimal.Decimal
}

func (a cashAdjustments) net() decimal.Decimal {
	return a.ExchangeIn.Sub(a.ExchangeOut).Sub(a.Refunds)
}

func expectedBalances(openingCash decimal.Decimal, totals []paymentTotal, adjustments cashAdjustments) (map[ReconciliationMethod]decimal.Decimal, error) {
	expected := map[ReconciliationMethod]decimal.Decimal{
		ReconciliationMethodCash: openingCash.Add(adjustments.net()),
	}
	for _, total := range totals {
		classified := false
		for _, rule := range paymentRules {
			if total.Method == rule.Payment {
				expected[rule.Reconciliation] = expected[rule.Reconciliation].Add(total.Total)
				classified = true
				break
			}
		}
		if !classified {
			return nil, common.ConflictError("unsupported payment method in shift: " + string(total.Method))
		}
	}
	return expected, nil
}

// calculateReconciliation is pure: database reads, locks and writes belong
// to the caller. Cash is counted; electronic settlement matches its expected
// amount. A cash discrepancy requires a reason.
func calculateReconciliation(shift Shift, totals []paymentTotal, adjustments cashAdjustments, in CloseShiftRequest) ([]ShiftReconciliation, error) {
	expected, err := expectedBalances(shift.OpeningCash, totals, adjustments)
	if err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(in.Reason)
	rows := make([]ShiftReconciliation, 0, len(expected))
	for _, rule := range paymentRules {
		amount, exists := expected[rule.Reconciliation]
		if !exists {
			continue
		}
		row := ShiftReconciliation{
			ShiftID: shift.ID, Method: rule.Reconciliation,
			Expected: amount, Counted: amount, Difference: decimal.Zero,
		}
		if rule.CountedInDrawer {
			row.Counted = in.ClosingCash
			row.Difference = in.ClosingCash.Sub(amount)
			row.Reason = reason
			if !row.Difference.IsZero() && reason == "" {
				return nil, common.BadRequestError("reason is required when closing_cash does not match the expected cash total")
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
