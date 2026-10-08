package reports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"shagan_pos/internal/common"
)

type Service struct {
	repo     Repository
	branches BranchLookup
}

func NewService(repo Repository, branches BranchLookup) *Service {
	return &Service{repo: repo, branches: branches}
}

// resolveBranchIDs turns branchID (one verified branch, or every branch in
// orgID when nil) into the branchIDs GetProfitAndLoss's Expenses query
// expects - same reasoning as inventory.Service.resolveBranchIDs. Every
// other Reports query filters its own table's plain org_id column instead
// and never needed this.
func (s *Service) resolveBranchIDs(ctx context.Context, orgID uint, branchID *uint) ([]uint, error) {
	if branchID != nil {
		if _, err := s.branches.GetBranch(ctx, orgID, *branchID); err != nil {
			return nil, err
		}
		return []uint{*branchID}, nil
	}
	branchList, err := s.branches.ListBranches(ctx, orgID)
	if err != nil {
		return nil, err
	}
	branchIDs := make([]uint, len(branchList))
	for i, b := range branchList {
		branchIDs[i] = b.ID
	}
	return branchIDs, nil
}

var _ Interface = (*Service)(nil)

const defaultLookbackDays = 30

// zone is an organization's calendar: the IANA name (for SQL bucketing) and
// its resolved location (for date math).
type zone struct {
	name string
	loc  *time.Location
}

// orgZone reads orgID's timezone - the zone that defines its calendar day, so
// "today", a report's from/to dates and its daily buckets all fall on the
// org's own midnight instead of UTC's (for Myanmar, UTC+6:30, "today" would
// otherwise roll over at 06:30 local).
func (s *Service) orgZone(ctx context.Context, orgID uint) (zone, error) {
	org, err := s.branches.GetOrganization(ctx, orgID)
	if err != nil {
		return zone{}, err
	}
	loc := common.OrgLocation(org.Timezone)
	return zone{name: loc.String(), loc: loc}, nil
}

// todayRange is the [start, end) window for "today" in loc - used by
// GetHomeSummary/GetTodayReport, which always mean today, never a
// client-supplied range.
func todayRange(loc *time.Location) (time.Time, time.Time) {
	start := common.StartOfDay(time.Now().In(loc), loc)
	return start, common.NextDay(start)
}

// resolveDateRange turns an inclusive, optional [from, to] calendar-date
// pair into the exclusive [start, end) window every repository query
// expects, with each date read as a day in loc (its own local midnight) -
// to's whole day is included by advancing it one day. Defaults to the last
// defaultLookbackDays days, ending today in loc, when either bound is
// omitted.
func resolveDateRange(loc *time.Location, from, to *time.Time) (time.Time, time.Time) {
	end := common.StartOfDay(time.Now().In(loc), loc)
	if to != nil {
		end = common.StartOfDay(*to, loc)
	}
	end = common.NextDay(end)

	start := end.AddDate(0, 0, -defaultLookbackDays)
	if from != nil {
		start = common.StartOfDay(*from, loc)
	}
	return start, end
}

// window is resolveDateRange for the common case that needs nothing but the
// bounds.
func (s *Service) window(ctx context.Context, orgID uint, from, to *time.Time) (time.Time, time.Time, error) {
	z, err := s.orgZone(ctx, orgID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start, end := resolveDateRange(z.loc, from, to)
	return start, end, nil
}

// calendarDate is t's local calendar date as a UTC-midnight value - for
// columns that hold a plain DATE (Expense.Date), which must be compared as
// dates, never as the instants a local midnight denotes.
func calendarDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
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
	z, err := s.orgZone(ctx, orgID)
	if err != nil {
		return nil, err
	}
	start, end := todayRange(z.loc)
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
	z, err := s.orgZone(ctx, orgID)
	if err != nil {
		return nil, err
	}
	start, end := todayRange(z.loc)

	totals, err := s.salesTotals(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}

	hourlyBuckets, err := s.repo.HourlyTrend(ctx, orgID, branchID, start, end, z.name)
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
	z, err := s.orgZone(ctx, orgID)
	if err != nil {
		return nil, err
	}
	start, end := resolveDateRange(z.loc, from, to)
	buckets, err := s.repo.Trend(ctx, orgID, branchID, start, end, g, z.name)
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
	start, end, err := s.window(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}

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
	z, err := s.orgZone(ctx, orgID)
	if err != nil {
		return nil, err
	}
	start, end := resolveDateRange(z.loc, from, to)
	buckets, err := s.repo.Trend(ctx, orgID, branchID, start, end, g, z.name)
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
	start, end, err := s.window(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
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
	start, end, err := s.window(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	page, pageSize = resolvePagination(page, pageSize)
	transactions, total, err := s.repo.ListTransactions(ctx, orgID, branchID, start, end, page, pageSize)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(transactions))
	for i, t := range transactions {
		ids[i] = t.ID
	}
	methods, err := s.repo.PaymentMethodsBySale(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range transactions {
		m := methods[transactions[i].ID]
		if m == nil {
			m = []string{}
		}
		transactions[i].PaymentMethods = m
	}
	return &TransactionsReport{
		Transactions: transactions,
		Page:         page,
		PageSize:     pageSize,
		TotalCount:   total,
	}, nil
}

func (s *Service) GetProductSalesReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, categoryID *uint) (*ProductSalesReport, error) {
	start, end, err := s.window(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	products, err := s.repo.ProductSales(ctx, orgID, branchID, start, end, categoryID, nil)
	if err != nil {
		return nil, err
	}
	return &ProductSalesReport{Products: products}, nil
}

func (s *Service) GetTopProducts(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, limit *int) (*TopProductsReport, error) {
	start, end, err := s.window(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}
	n := resolveLimit(limit)
	products, err := s.repo.ProductSales(ctx, orgID, branchID, start, end, nil, &n)
	if err != nil {
		return nil, err
	}
	return &TopProductsReport{Products: products}, nil
}

func (s *Service) GetProfitAndLoss(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time) (*ProfitAndLoss, error) {
	start, end, err := s.window(ctx, orgID, from, to)
	if err != nil {
		return nil, err
	}

	totals, err := s.salesTotals(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	cogs, err := s.repo.COGS(ctx, orgID, branchID, start, end)
	if err != nil {
		return nil, err
	}
	branchIDs, err := s.resolveBranchIDs(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	expenses, err := s.repo.Expenses(ctx, branchIDs, calendarDate(start), calendarDate(end))
	if err != nil {
		return nil, err
	}

	grossProfit := totals.NetSales.Sub(cogs)
	netProfit := grossProfit.Sub(expenses)
	return &ProfitAndLoss{
		SalesTotals: totals,
		COGS:        cogs,
		GrossProfit: grossProfit,
		Expenses:    expenses,
		NetProfit:   netProfit,
	}, nil
}

func (s *Service) ExportReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}
