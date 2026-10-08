package reports

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

func requireRestErrorStatus(t *testing.T, err error, status int) {
	t.Helper()
	var restErr common.RestError
	require.True(t, errors.As(err, &restErr), "expected a common.RestError, got %T: %v", err, err)
	require.Equal(t, status, restErr.Status)
}

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

// --- pure helper functions ---

func TestResolveDateRange_NoBounds_DefaultsToLast30Days(t *testing.T) {
	start, end := resolveDateRange(nil, nil)
	wantEnd := startOfDayUTC(time.Now().UTC()).Add(24 * time.Hour)
	require.Equal(t, wantEnd, end)
	require.Equal(t, wantEnd.AddDate(0, 0, -30), start)
}

func TestResolveDateRange_ExplicitFromAndTo_ToIsExclusiveUpperBound(t *testing.T) {
	from := time.Date(2026, 1, 1, 15, 30, 0, 0, time.UTC)
	to := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	start, end := resolveDateRange(&from, &to)
	require.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), start)
	require.Equal(t, time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC), end)
}

func TestTodayRangeUTC_YieldsOneDayWindow(t *testing.T) {
	start, end := todayRangeUTC()
	require.Equal(t, 24*time.Hour, end.Sub(start))
	require.Equal(t, startOfDayUTC(time.Now().UTC()), start)
}

func TestResolveGranularity_EmptyDefaultsToDaily(t *testing.T) {
	g, err := resolveGranularity("")
	require.NoError(t, err)
	require.Equal(t, GranularityDaily, g)
}

func TestResolveGranularity_ValidValuesPassThrough(t *testing.T) {
	for _, in := range []Granularity{GranularityDaily, GranularityWeekly, GranularityMonthly} {
		g, err := resolveGranularity(in)
		require.NoError(t, err)
		require.Equal(t, in, g)
	}
}

func TestResolveGranularity_Invalid_ReturnsBadRequest(t *testing.T) {
	_, err := resolveGranularity("hourly")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestResolvePagination_DefaultsAndCaps(t *testing.T) {
	page, pageSize := resolvePagination(0, 0)
	require.Equal(t, 1, page)
	require.Equal(t, 20, pageSize)

	page, pageSize = resolvePagination(-1, 500)
	require.Equal(t, 1, page)
	require.Equal(t, 100, pageSize)

	page, pageSize = resolvePagination(3, 15)
	require.Equal(t, 3, page)
	require.Equal(t, 15, pageSize)
}

func TestResolveLimit_DefaultsAndCaps(t *testing.T) {
	require.Equal(t, 10, resolveLimit(nil))
	zero := 0
	require.Equal(t, 10, resolveLimit(&zero))
	big := 500
	require.Equal(t, 100, resolveLimit(&big))
	five := 5
	require.Equal(t, 5, resolveLimit(&five))
}

func TestApplyPercentages_SplitsProportionally(t *testing.T) {
	methods := []PaymentMethodBreakdown{
		{Method: "cash", Amount: d("75.00")},
		{Method: "card", Amount: d("25.00")},
	}
	applyPercentages(methods)
	require.True(t, methods[0].Percentage.Equal(d("75")))
	require.True(t, methods[1].Percentage.Equal(d("25")))
}

func TestApplyPercentages_ZeroTotal_LeavesPercentagesZero(t *testing.T) {
	methods := []PaymentMethodBreakdown{{Method: "cash", Amount: decimal.Zero}}
	applyPercentages(methods)
	require.True(t, methods[0].Percentage.IsZero())
}

// --- service methods ---

func TestService_GetHomeSummary_ComposesTotalsAndLowStockCount(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(SalesAggregate{Gross: d("100.00"), Discounts: d("10.00"), Count: 5}, nil).Once()
	repo.EXPECT().ReturnsTotal(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(d("5.00"), nil).Once()
	repo.EXPECT().ProductCounts(mock.Anything, uint(7), (*uint)(nil)).
		Return(StockOverview{TotalProducts: 20, LowStockCount: 3, OutOfStockCount: 1}, nil).Once()

	got, err := svc.GetHomeSummary(context.Background(), 7, nil)
	require.NoError(t, err)
	require.True(t, got.GrossSales.Equal(d("100.00")))
	require.True(t, got.Discounts.Equal(d("10.00")))
	require.True(t, got.Returns.Equal(d("5.00")))
	require.True(t, got.NetSales.Equal(d("85.00")))
	require.Equal(t, 5, got.TransactionCount)
	require.Equal(t, 3, got.LowStockCount)
}

func TestService_GetHomeSummary_PropagatesRepositoryError(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	wantErr := common.SystemError("db read failed")
	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(SalesAggregate{}, wantErr).Once()

	_, err := svc.GetHomeSummary(context.Background(), 7, nil)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_GetStockOverview_DelegatesToRepository(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	branchID := uint(5)
	want := StockOverview{TotalProducts: 10, LowStockCount: 2, OutOfStockCount: 0}
	repo.EXPECT().ProductCounts(mock.Anything, uint(7), &branchID).Return(want, nil).Once()

	got, err := svc.GetStockOverview(context.Background(), 7, &branchID)
	require.NoError(t, err)
	require.Equal(t, want, *got)
}

func TestService_GetTodayReport_ComposesAllFourQueries(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(SalesAggregate{Gross: d("50.00"), Discounts: d("0"), Count: 2}, nil).Once()
	repo.EXPECT().ReturnsTotal(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(decimal.Zero, nil).Once()
	repo.EXPECT().HourlyTrend(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return([]TrendBucket{{Period: "09:00", Revenue: d("50.00"), Count: 2}}, nil).Once()
	repo.EXPECT().PaymentMethodBreakdown(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return([]PaymentMethodBreakdown{{Method: "cash", Amount: d("50.00"), Count: 2}}, nil).Once()
	repo.EXPECT().
		ProductSales(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, (*uint)(nil), mock.MatchedBy(func(l *int) bool { return l != nil && *l == 5 })).
		Return([]ProductRevenue{{ProductID: 1, Name: "Cola", QtySold: 2, Revenue: d("50.00")}}, nil).Once()

	got, err := svc.GetTodayReport(context.Background(), 7, nil)
	require.NoError(t, err)
	require.True(t, got.NetSales.Equal(d("50.00")))
	require.Len(t, got.HourlyTrend, 1)
	require.Equal(t, "09:00", got.HourlyTrend[0].Hour)
	require.Len(t, got.PaymentMethods, 1)
	require.True(t, got.PaymentMethods[0].Percentage.Equal(d("100")))
	require.Len(t, got.TopProducts, 1)
	require.Equal(t, "Cola", got.TopProducts[0].Name)
}

func TestService_GetRevenueTrend_MapsBucketsToPoints(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().Trend(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, GranularityDaily).
		Return([]TrendBucket{{Period: "2026-01-01", Revenue: d("20.00"), Count: 1}}, nil).Once()

	got, err := svc.GetRevenueTrend(context.Background(), 7, nil, nil, nil, "")
	require.NoError(t, err)
	require.Equal(t, GranularityDaily, got.Granularity)
	require.Len(t, got.Points, 1)
	require.Equal(t, "2026-01-01", got.Points[0].Period)
	require.True(t, got.Points[0].NetSales.Equal(d("20.00")))
}

func TestService_GetRevenueTrend_InvalidGranularity_ReturnsBadRequestWithoutQuerying(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)
	// Trend must never be called - rejected before any query.

	_, err := svc.GetRevenueTrend(context.Background(), 7, nil, nil, nil, "hourly")
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusBadRequest)
}

func TestService_GetSalesSummary_ComposesTotalsAndAllThreeBreakdowns(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(SalesAggregate{Gross: d("200.00"), Discounts: d("20.00"), Count: 10}, nil).Once()
	repo.EXPECT().ReturnsTotal(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(d("10.00"), nil).Once()
	repo.EXPECT().BranchBreakdown(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return([]BranchBreakdown{{BranchID: 5, BranchName: "Main", NetSales: d("180.00")}}, nil).Once()
	repo.EXPECT().PaymentMethodBreakdown(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return([]PaymentMethodBreakdown{{Method: "cash", Amount: d("200.00"), Count: 10}}, nil).Once()
	repo.EXPECT().CategoryBreakdown(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return([]CategoryBreakdown{{CategoryID: 1, CategoryName: "Beverages", NetSales: d("180.00")}}, nil).Once()

	got, err := svc.GetSalesSummary(context.Background(), 7, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, got.NetSales.Equal(d("170.00")))
	require.Len(t, got.ByBranch, 1)
	require.Len(t, got.ByPaymentMethod, 1)
	require.True(t, got.ByPaymentMethod[0].Percentage.Equal(d("100")))
	require.Len(t, got.ByCategory, 1)
}

func TestService_GetSalesTrend_MapsBucketsToPointsWithCount(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().Trend(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, GranularityWeekly).
		Return([]TrendBucket{{Period: "2026-01-01", Revenue: d("300.00"), Count: 15}}, nil).Once()

	got, err := svc.GetSalesTrend(context.Background(), 7, nil, nil, nil, GranularityWeekly)
	require.NoError(t, err)
	require.Equal(t, GranularityWeekly, got.Granularity)
	require.Equal(t, 15, got.Points[0].TransactionCount)
	require.True(t, got.Points[0].NetSales.Equal(d("300.00")))
}

func TestService_GetPaymentMethodsReport_ComputesTotalAndPercentages(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().PaymentMethodBreakdown(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return([]PaymentMethodBreakdown{
			{Method: "cash", Amount: d("60.00"), Count: 3},
			{Method: "card", Amount: d("40.00"), Count: 2},
		}, nil).Once()

	got, err := svc.GetPaymentMethodsReport(context.Background(), 7, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, got.Total.Equal(d("100.00")))
	require.True(t, got.Methods[0].Percentage.Equal(d("60")))
	require.True(t, got.Methods[1].Percentage.Equal(d("40")))
}

func TestService_GetTransactionsReport_DefaultsPagination(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().
		ListTransactions(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, 1, 20).
		Return([]TransactionSummary{}, int64(0), nil).Once()
	repo.EXPECT().PaymentMethodsBySale(mock.Anything, mock.Anything).Return(map[uuid.UUID][]string{}, nil).Once()

	got, err := svc.GetTransactionsReport(context.Background(), 7, nil, nil, nil, 0, 0)
	require.NoError(t, err)
	require.Equal(t, 1, got.Page)
	require.Equal(t, 20, got.PageSize)
}

func TestService_GetTransactionsReport_PassesThroughExplicitPagination(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().
		ListTransactions(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, 2, 50).
		Return([]TransactionSummary{}, int64(120), nil).Once()
	repo.EXPECT().PaymentMethodsBySale(mock.Anything, mock.Anything).Return(map[uuid.UUID][]string{}, nil).Once()

	got, err := svc.GetTransactionsReport(context.Background(), 7, nil, nil, nil, 2, 50)
	require.NoError(t, err)
	require.Equal(t, 2, got.Page)
	require.Equal(t, 50, got.PageSize)
	require.Equal(t, int64(120), got.TotalCount)
}

func TestService_GetTransactionsReport_AttachesPaymentMethodsPerRow(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	cashOnly, split, none := uuid.New(), uuid.New(), uuid.New()
	repo.EXPECT().
		ListTransactions(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, 1, 20).
		Return([]TransactionSummary{{ID: cashOnly}, {ID: split}, {ID: none}}, int64(3), nil).Once()
	// One query for the whole page, not one per row.
	repo.EXPECT().PaymentMethodsBySale(mock.Anything, []uuid.UUID{cashOnly, split, none}).
		Return(map[uuid.UUID][]string{cashOnly: {"cash"}, split: {"cash", "qr"}}, nil).Once()

	got, err := svc.GetTransactionsReport(context.Background(), 7, nil, nil, nil, 1, 20)
	require.NoError(t, err)
	require.Equal(t, []string{"cash"}, got.Transactions[0].PaymentMethods)
	require.Equal(t, []string{"cash", "qr"}, got.Transactions[1].PaymentMethods)
	require.Equal(t, []string{}, got.Transactions[2].PaymentMethods, "no payment rows -> [], not null")
}

func TestService_GetTransactionsReport_PaymentMethodsErrorPropagates(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	boom := errors.New("db down")
	repo.EXPECT().ListTransactions(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, 1, 20).
		Return([]TransactionSummary{{ID: uuid.New()}}, int64(1), nil).Once()
	repo.EXPECT().PaymentMethodsBySale(mock.Anything, mock.Anything).Return(nil, boom).Once()

	_, err := svc.GetTransactionsReport(context.Background(), 7, nil, nil, nil, 1, 20)
	require.ErrorIs(t, err, boom)
}

func TestService_GetProductSalesReport_PassesCategoryIDThrough(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	categoryID := uint(3)
	repo.EXPECT().
		ProductSales(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, &categoryID, (*int)(nil)).
		Return([]ProductRevenue{{ProductID: 1, Name: "Cola", QtySold: 4, Revenue: d("8.00")}}, nil).Once()

	got, err := svc.GetProductSalesReport(context.Background(), 7, nil, nil, nil, &categoryID)
	require.NoError(t, err)
	require.Len(t, got.Products, 1)
}

func TestService_GetTopProducts_DefaultsLimitToTen(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().
		ProductSales(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, (*uint)(nil), mock.MatchedBy(func(l *int) bool { return l != nil && *l == 10 })).
		Return([]ProductRevenue{}, nil).Once()

	_, err := svc.GetTopProducts(context.Background(), 7, nil, nil, nil, nil)
	require.NoError(t, err)
}

func TestService_GetTopProducts_PassesThroughExplicitLimit(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	limit := 3
	repo.EXPECT().
		ProductSales(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything, (*uint)(nil), mock.MatchedBy(func(l *int) bool { return l != nil && *l == 3 })).
		Return([]ProductRevenue{}, nil).Once()

	_, err := svc.GetTopProducts(context.Background(), 7, nil, nil, nil, &limit)
	require.NoError(t, err)
}

func TestService_GetProfitAndLoss_OrgWide_ComposesAllFourFigures(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	orgBranches := []identity.Branch{{ID: 5, OrgID: 7}, {ID: 6, OrgID: 7}}
	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(SalesAggregate{Gross: d("500.00"), Discounts: d("20.00"), Count: 10}, nil).Once()
	repo.EXPECT().ReturnsTotal(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(d("30.00"), nil).Once()
	repo.EXPECT().COGS(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(d("200.00"), nil).Once()
	branches.EXPECT().ListBranches(mock.Anything, uint(7)).Return(orgBranches, nil).Once()
	repo.EXPECT().Expenses(mock.Anything, []uint{5, 6}, mock.Anything, mock.Anything).
		Return(d("50.00"), nil).Once()

	got, err := svc.GetProfitAndLoss(context.Background(), 7, nil, nil, nil)
	require.NoError(t, err)
	// NetSales = 500 - 20 - 30 = 450
	require.True(t, got.NetSales.Equal(d("450.00")))
	require.True(t, got.COGS.Equal(d("200.00")))
	// GrossProfit = 450 - 200 = 250
	require.True(t, got.GrossProfit.Equal(d("250.00")))
	require.True(t, got.Expenses.Equal(d("50.00")))
	// NetProfit = 250 - 50 = 200
	require.True(t, got.NetProfit.Equal(d("200.00")))
}

func TestService_GetProfitAndLoss_ScopedToOneBranch(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	branchID := uint(5)
	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), &branchID, mock.Anything, mock.Anything).
		Return(SalesAggregate{Gross: d("100.00"), Discounts: d("0"), Count: 2}, nil).Once()
	repo.EXPECT().ReturnsTotal(mock.Anything, uint(7), &branchID, mock.Anything, mock.Anything).
		Return(decimal.Zero, nil).Once()
	repo.EXPECT().COGS(mock.Anything, uint(7), &branchID, mock.Anything, mock.Anything).
		Return(d("40.00"), nil).Once()
	branches.EXPECT().GetBranch(mock.Anything, uint(7), uint(5)).Return(&identity.Branch{ID: 5, OrgID: 7}, nil).Once()
	repo.EXPECT().Expenses(mock.Anything, []uint{5}, mock.Anything, mock.Anything).
		Return(d("10.00"), nil).Once()

	got, err := svc.GetProfitAndLoss(context.Background(), 7, &branchID, nil, nil)
	require.NoError(t, err)
	require.True(t, got.NetSales.Equal(d("100.00")))
	require.True(t, got.GrossProfit.Equal(d("60.00")))
	require.True(t, got.NetProfit.Equal(d("50.00")))
}

func TestService_GetProfitAndLoss_PropagatesCOGSError(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	repo.EXPECT().SalesAggregate(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(SalesAggregate{Gross: d("100.00")}, nil).Once()
	repo.EXPECT().ReturnsTotal(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(decimal.Zero, nil).Once()
	wantErr := common.SystemError("db read failed")
	repo.EXPECT().COGS(mock.Anything, uint(7), (*uint)(nil), mock.Anything, mock.Anything).
		Return(decimal.Zero, wantErr).Once()
	// Expenses must never be called - rejected before that query happens.

	_, err := svc.GetProfitAndLoss(context.Background(), 7, nil, nil, nil)
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusInternalServerError)
}

func TestService_ExportReport_StaysNotImplemented(t *testing.T) {
	repo := NewMockRepository(t)
	branches := NewMockBranchLookup(t)
	svc := NewService(repo, branches)

	_, err := svc.ExportReport(context.Background())
	require.Error(t, err)
	requireRestErrorStatus(t, err, http.StatusNotImplemented)
}
