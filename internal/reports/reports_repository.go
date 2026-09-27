package reports

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// SalesAggregate is the raw Gross Sales/Discounts/count population every
// headline figure is built from - completed, non-voided sales in a
// [from, to) window, optionally scoped to one branch. Returns isn't here -
// see ReturnsTotal.
type SalesAggregate struct {
	Gross     decimal.Decimal
	Discounts decimal.Decimal
	Count     int
}

// ProductCounts backs GetStockOverview directly - same shape as the
// StockOverview response, so the service just returns it as-is.
type ProductCounts = StockOverview

// Repository defines the reports domain's persistence operations. Every
// method is a plain, scoped query - no business decision about what counts
// as "revenue" or how Net Sales is derived happens here, see
// Service's doc comments for that.
type Repository interface {
	// SalesAggregate is the core Gross/Discounts/count query - status NOT IN
	// (voided, open), completed_at within [from, to), optionally one branch.
	SalesAggregate(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) (SalesAggregate, error)
	// ReturnsTotal sums Return.RefundTotal for returns recorded (by their own
	// CreatedAt, not the original sale's date) against sales in orgID/
	// branchID, within [from, to).
	ReturnsTotal(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) (decimal.Decimal, error)
	// HourlyTrend backs GetTodayReport only - Gross-Discounts bucketed by
	// hour of day (as "HH:00"), for the single day [dayStart, dayEnd).
	HourlyTrend(ctx context.Context, orgID uint, branchID *uint, dayStart, dayEnd time.Time) ([]TrendBucket, error)
	// Trend backs GetRevenueTrend/GetSalesTrend - Gross-Discounts and count
	// bucketed by granularity (day/week/month, as "YYYY-MM-DD") across
	// [from, to).
	Trend(ctx context.Context, orgID uint, branchID *uint, from, to time.Time, granularity Granularity) ([]TrendBucket, error)
	// BranchBreakdown backs SalesSummary.ByBranch - Gross-Discounts grouped
	// by branch. A branch-scoped caller gets back exactly one row (its own).
	BranchBreakdown(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) ([]BranchBreakdown, error)
	// CategoryBreakdown backs SalesSummary.ByCategory - SUM(SaleItem.LineTotal)
	// grouped by the product's own category.
	CategoryBreakdown(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) ([]CategoryBreakdown, error)
	// PaymentMethodBreakdown backs TodayReport/SalesSummary/
	// PaymentMethodsReport - actual Payment.Amount grouped by method (see
	// PaymentMethodBreakdown's own doc for why this is tax-inclusive).
	// Percentage is always zero here - the service computes it once every
	// method's amount is known.
	PaymentMethodBreakdown(ctx context.Context, orgID uint, branchID *uint, from, to time.Time) ([]PaymentMethodBreakdown, error)
	// ListTransactions backs GetTransactionsReport - a page of raw sale rows
	// ordered newest-first, plus the total count across every page.
	ListTransactions(ctx context.Context, orgID uint, branchID *uint, from, to time.Time, page, pageSize int) ([]TransactionSummary, int64, error)
	// ProductSales backs GetProductSalesReport/GetTopProducts -
	// SUM(SaleItem.Qty)/SUM(SaleItem.LineTotal) grouped by product, ranked
	// by revenue descending. categoryID optionally narrows to one category;
	// limit optionally caps the row count (nil for every product).
	ProductSales(ctx context.Context, orgID uint, branchID *uint, from, to time.Time, categoryID *uint, limit *int) ([]ProductRevenue, error)
	// ProductCounts backs GetStockOverview - total product count, and how
	// many have fallen to or below their own reorder threshold / to zero,
	// scoped to orgID and optionally one branch.
	ProductCounts(ctx context.Context, orgID uint, branchID *uint) (ProductCounts, error)
}
