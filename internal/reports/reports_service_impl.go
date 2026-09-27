package reports

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"shagan_pos/internal/common"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

const defaultLookbackDays = 30

func startOfDayUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// todayRangeUTC is the [start, end) window for "today" - used by
// GetHomeSummary/GetTodayReport, which always mean today, never a
// client-supplied range.
func todayRangeUTC() (time.Time, time.Time) {
	start := startOfDayUTC(time.Now().UTC())
	return start, start.Add(24 * time.Hour)
}

// resolveDateRange turns an inclusive, optional [from, to] calendar-date
// pair into the exclusive [start, end) window every repository query
// expects - to's whole day is included by advancing it 24h. Defaults to the
// last defaultLookbackDays days when either bound is omitted.
func resolveDateRange(from, to *time.Time) (time.Time, time.Time) {
	end := startOfDayUTC(time.Now().UTC())
	if to != nil {
		end = startOfDayUTC(*to)
	}
	end = end.Add(24 * time.Hour)

	start := end.AddDate(0, 0, -defaultLookbackDays)
	if from != nil {
		start = startOfDayUTC(*from)
	}
	return start, end
}

func resolveGranularity(g Granularity) (Granularity, error) {
	switch g {
	case "", GranularityDaily:
		return GranularityDaily, nil
	case GranularityWeekly:
		return GranularityWeekly, nil
	case GranularityMonthly:
		return GranularityMonthly, nil
	default:
		return "", common.BadRequestError("granularity must be one of: daily, weekly, monthly")
	}
}

func resolvePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func resolveLimit(limit *int) int {
	if limit == nil || *limit <= 0 {
		return 10
	}
	if *limit > 100 {
		return 100
	}
	return *limit
}

// salesTotals composes Repository.SalesAggregate and Repository.ReturnsTotal
// into the Gross/Discounts/Returns/Net breakdown - see SalesTotals's own doc
// for the formula.
func (s *Service) salesTotals(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) (SalesTotals, error) {
	agg, err := s.repo.SalesAggregate(ctx, orgID, branchID, from, to)
	if err != nil {
		return SalesTotals{}, err
	}
	returns, err := s.repo.ReturnsTotal(ctx, orgID, branchID, from, to)
	if err != nil {
		return SalesTotals{}, err
	}
	return SalesTotals{
		GrossSales:       agg.Gross,
		Discounts:        agg.Discounts,
		Returns:          returns,
		NetSales:         agg.Gross.Sub(agg.Discounts).Sub(returns),
		TransactionCount: agg.Count,
	}, nil
}

// applyPercentages fills in each method's share of the combined total -
// Repository.PaymentMethodBreakdown always returns it zeroed, since the
// total is only known once every method's amount has been fetched.
func applyPercentages(methods []PaymentMethodBreakdown) {
	total := decimal.Zero
	for _, m := range methods {
		total = total.Add(m.Amount)
	}
	if total.IsZero() {
		return
	}
	for i := range methods {
		methods[i].Percentage = methods[i].Amount.Div(total).Mul(decimal.NewFromInt(100)).Round(2)
	}
}

func (s *Service) GetHomeSummary(ctx context.Context, orgID uint, branchID *uint) (*HomeSummary, error) {
	start, end := todayRangeUTC()
	totals, err := s.salesTotals(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.ProductCounts(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return &HomeSummary{SalesTotals: totals, LowStockCount: counts.LowStockCount}, nil
}

func (s *Service) GetStockOverview(ctx context.Context, orgID uint, branchID *uint) (*StockOverview, error) {
	counts, err := s.repo.ProductCounts(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return &counts, nil
}

func (s *Service) GetTodayReport(ctx context.Context, orgID uint, branchID *uint) (*TodayReport, error) {
	start, end := todayRangeUTC()

	totals, err := s.salesTotals(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}

	hourlyBuckets, err := s.repo.HourlyTrend(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	hourly := make([]HourlyPoint, len(hourlyBuckets))
	for i, b := range hourlyBuckets {
		hourly[i] = HourlyPoint{Hour: b.Period, NetSales: b.Revenue}
	}

	methods, err := s.repo.PaymentMethodBreakdown(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	applyPercentages(methods)

	topLimit := 5
	products, err := s.repo.ProductSales(ctx, orgID, branchID, start, end, nil, &topLimit)
	if err != nil {
		return nil, err
	}

	return &TodayReport{
		SalesTotals:    totals,
		HourlyTrend:    hourly,
		PaymentMethods: methods,
		TopProducts:    products,
	}, nil
}

func (s *Service) GetRevenueTrend(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, granularity Granularity) (*RevenueTrend, error) {
	g, err := resolveGranularity(granularity)
	if err != nil {
		return nil, err
	}
	start, end := resolveDateRange(from, to)
	buckets, err := s.repo.Trend(ctx, orgID, branchID, start, end, g)
	if err != nil {
		return nil, err
	}
	points := make([]TrendPoint, len(buckets))
	for i, b := range buckets {
		points[i] = TrendPoint{Period: b.Period, NetSales: b.Revenue}
	}
	return &RevenueTrend{Granularity: g, Points: points}, nil
}

func (s *Service) GetSalesSummary(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time) (*SalesSummary, error) {
	start, end := resolveDateRange(from, to)

	totals, err := s.salesTotals(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	byBranch, err := s.repo.BranchBreakdown(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	byMethod, err := s.repo.PaymentMethodBreakdown(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	applyPercentages(byMethod)
	byCategory, err := s.repo.CategoryBreakdown(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}

	return &SalesSummary{
		SalesTotals:     totals,
		ByBranch:        byBranch,
		ByPaymentMethod: byMethod,
		ByCategory:      byCategory,
	}, nil
}

func (s *Service) GetSalesTrend(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, granularity Granularity) (*SalesTrend, error) {
	g, err := resolveGranularity(granularity)
	if err != nil {
		return nil, err
	}
	start, end := resolveDateRange(from, to)
	buckets, err := s.repo.Trend(ctx, orgID, branchID, start, end, g)
	if err != nil {
		return nil, err
	}
	points := make([]SalesTrendPoint, len(buckets))
	for i, b := range buckets {
		points[i] = SalesTrendPoint{Period: b.Period, TransactionCount: b.Count, NetSales: b.Revenue}
	}
	return &SalesTrend{Granularity: g, Points: points}, nil
}

func (s *Service) GetPaymentMethodsReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time) (*PaymentMethodsReport, error) {
	start, end := resolveDateRange(from, to)
	methods, err := s.repo.PaymentMethodBreakdown(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	applyPercentages(methods)
	total := decimal.Zero
	for _, m := range methods {
		total = total.Add(m.Amount)
	}
	return &PaymentMethodsReport{Total: total, Methods: methods}, nil
}

func (s *Service) GetTransactionsReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, page, pageSize int) (*TransactionsReport, error) {
	start, end := resolveDateRange(from, to)
	page, pageSize = resolvePagination(page, pageSize)
	transactions, total, err := s.repo.ListTransactions(ctx, orgID, branchID, start, end, page, pageSize)
	if err != nil {
		return nil, err
	}
	return &TransactionsReport{
		Transactions: transactions,
		Page:         page,
		PageSize:     pageSize,
		TotalCount:   total,
	}, nil
}

func (s *Service) GetProductSalesReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, categoryID *uint) (*ProductSalesReport, error) {
	start, end := resolveDateRange(from, to)
	products, err := s.repo.ProductSales(ctx, orgID, branchID, start, end, categoryID, nil)
	if err != nil {
		return nil, err
	}
	return &ProductSalesReport{Products: products}, nil
}

func (s *Service) GetTopProducts(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, limit *int) (*TopProductsReport, error) {
	start, end := resolveDateRange(from, to)
	n := resolveLimit(limit)
	products, err := s.repo.ProductSales(ctx, orgID, branchID, start, end, nil, &n)
	if err != nil {
		return nil, err
	}
	return &TopProductsReport{Products: products}, nil
}

func (s *Service) ExportReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}
