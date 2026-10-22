package reports

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func sampleProfitAndLoss() ProfitAndLoss {
	return ProfitAndLoss{
		SalesTotals: SalesTotals{
			GrossSales: decimal.NewFromInt(1000), Discounts: decimal.NewFromInt(50),
			Returns: decimal.NewFromInt(30), NetSales: decimal.NewFromInt(920), TransactionCount: 12,
		},
		COGS: decimal.NewFromInt(500), GrossProfit: decimal.NewFromInt(420),
		Expenses: decimal.NewFromInt(100), NetProfit: decimal.NewFromInt(320),
	}
}

// Cost and profit are what view_item_cost protects: a caller without it
// still gets the sales side of the statement, but never COGS or either profit
// line (net profit would give COGS away by subtraction).
func TestProfitAndLoss_WithoutCost_OmitsCogsAndBothProfitLines(t *testing.T) {
	raw, err := json.Marshal(sampleProfitAndLoss().WithoutCost())
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	require.NotContains(t, got, "cogs")
	require.NotContains(t, got, "gross_profit")
	require.NotContains(t, got, "net_profit")
	require.Equal(t, "1000", got["gross_sales"])
	require.Equal(t, "920", got["net_sales"])
	require.Equal(t, "100", got["expenses"])
	require.EqualValues(t, 12, got["transaction_count"])
}

func TestProfitAndLoss_FullStatement_StillIncludesCost(t *testing.T) {
	raw, err := json.Marshal(sampleProfitAndLoss())
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, "500", got["cogs"])
	require.Equal(t, "420", got["gross_profit"])
	require.Equal(t, "320", got["net_profit"])
}
