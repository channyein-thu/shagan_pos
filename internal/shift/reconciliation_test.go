package shift

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/identity"
	"shagan_pos/internal/sales"
)

func TestReconciliation_UsesSameExpectedBalancesAsSummary(t *testing.T) {
	money := decimal.NewFromInt
	shift := Shift{ID: 4, OpeningCash: money(100)}
	totals := []paymentTotal{{Method: sales.PaymentMethodCash, Total: money(50)}, {Method: sales.PaymentMethodQR, Total: money(20)}}
	adjustments := cashAdjustments{Refunds: money(10), ExchangeIn: money(5), ExchangeOut: money(2)}
	expected, err := expectedBalances(shift.OpeningCash, totals, adjustments)
	require.NoError(t, err)
	require.True(t, money(143).Equal(expected[ReconciliationMethodCash]))
	rows, err := calculateReconciliation(shift, totals, adjustments, CloseShiftRequest{ClosingCash: money(140), Reason: " short "})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.True(t, expected[row.Method].Equal(row.Expected))
	}
	require.Equal(t, ReconciliationMethodCash, rows[0].Method)
	require.True(t, money(-3).Equal(rows[0].Difference))
	require.Equal(t, "short", rows[0].Reason)
	require.Equal(t, ReconciliationMethodQR, rows[1].Method)
	require.True(t, rows[1].Counted.Equal(rows[1].Expected))
	require.True(t, rows[1].Difference.IsZero())
	_, err = calculateReconciliation(shift, totals, adjustments, CloseShiftRequest{ClosingCash: money(140), Reason: " "})
	require.ErrorContains(t, err, "reason is required")
}

func TestReconciliation_RejectsUnknownPaymentInBothPaths(t *testing.T) {
	totals := []paymentTotal{{Method: sales.PaymentMethod("card"), Total: decimal.NewFromInt(10)}}
	_, err := expectedBalances(decimal.Zero, totals, cashAdjustments{})
	require.ErrorContains(t, err, "unsupported payment method")
	_, err = calculateReconciliation(Shift{}, totals, cashAdjustments{}, CloseShiftRequest{})
	require.ErrorContains(t, err, "unsupported payment method")
}

func TestReconciliation_OpeningFloatWithoutSales(t *testing.T) {
	amount := decimal.NewFromInt(100)
	rows, err := calculateReconciliation(Shift{OpeningCash: amount}, nil, cashAdjustments{}, CloseShiftRequest{ClosingCash: amount})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Expected.Equal(amount))
	require.True(t, rows[0].Difference.IsZero())
}

func TestLifecyclePolicies(t *testing.T) {
	branch := identity.Branch{Status: identity.BranchStatusActive}
	staff := identity.Staff{Status: identity.StaffStatusActive}
	device := identity.Device{Status: identity.DeviceStatusActive}
	require.NoError(t, validateOpeningShift(branch, staff, device, false))
	require.ErrorContains(t, validateOpeningShift(branch, staff, device, true), "already has an open shift")
	require.ErrorContains(t, validateOpeningShift(identity.Branch{}, staff, device, false), "branch is not active")
	require.ErrorContains(t, validateOpeningShift(branch, identity.Staff{}, device, false), "staff is not active")
	require.ErrorContains(t, validateOpeningShift(branch, staff, identity.Device{}, false), "device is not active")
	closer := uint(30)
	shift := Shift{Status: ShiftStatusOpen, StaffID: closer}
	require.NoError(t, validateShiftCloser(shift, &closer))
	other := uint(31)
	require.ErrorContains(t, validateShiftCloser(shift, &other), "only the staff member")
	require.NoError(t, validateShiftCloser(shift, nil), "force-close skips opener matching")
	shift.Status = ShiftStatusClosed
	require.ErrorContains(t, validateShiftCloser(shift, nil), "already closed")
	require.NoError(t, validateNoOpenSales(0))
	require.ErrorContains(t, validateNoOpenSales(1), "open sales")
}
