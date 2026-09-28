package reports

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Granularity is the time-bucket size for the trend endpoints
// (GetRevenueTrend/GetSalesTrend) - one of the values below, per the
// confirmed spec's daily/weekly/monthly requirement.
type Granularity string

const (
	GranularityDaily   Granularity = "daily"
	GranularityWeekly  Granularity = "weekly"
	GranularityMonthly Granularity = "monthly"
)

// Filter is the shared org/branch/date-range scope every report other than
// the always-today ones (GetHomeSummary/GetStockOverview/GetTodayReport) is
// queried against. BranchID is nil for an org-wide view (owner/
// service_center) - a branch-scoped caller's own branch always wins
// instead, same reasoning as inventory.Service.resolveBranchIDs. From/To
// are always populated by the service before a repository method ever sees
// them (query params default to the last 30 days when omitted).
type Filter struct {
	OrgID    uint
	BranchID *uint
	From     time.Time
	To       time.Time
}

// SalesTotals is the shared Gross/Discounts/Returns/Net breakdown behind
// every report that surfaces a headline revenue figure. NetSales = GrossSales
// - Discounts - Returns; tax is deliberately excluded from all three (tax is
// a pass-through, not revenue), and a Voided sale never enters GrossSales at
// all (a void erases the whole transaction, per docs/WORKFLOWS.md's Void
// rule) rather than being counted and subtracted back out.
type SalesTotals struct {
	GrossSales       decimal.Decimal `json:"gross_sales"`
	Discounts        decimal.Decimal `json:"discounts"`
	Returns          decimal.Decimal `json:"returns"`
	NetSales         decimal.Decimal `json:"net_sales"`
	TransactionCount int             `json:"transaction_count"`
}

// HomeSummary backs `GET /reports/home-summary` - the dashboard's header
// cards, always for today.
type HomeSummary struct {
	SalesTotals
	LowStockCount int `json:"low_stock_count"`
}

// StockOverview backs `GET /reports/stock-overview`.
type StockOverview struct {
	TotalProducts   int `json:"total_products"`
	LowStockCount   int `json:"low_stock_count"`
	OutOfStockCount int `json:"out_of_stock_count"`
}

// TrendBucket is the shared shape behind every time-bucketed query
// (HourlyTrend/Trend on Repository) - Revenue is GrossSales-Discounts for
// that bucket. Deliberately does not net out Returns per-bucket (see
// Service.GetRevenueTrend's doc) - not a public response shape itself, the
// service maps it into TrendPoint/SalesTrendPoint/HourlyPoint.
type TrendBucket struct {
	Period  string          `json:"-"`
	Revenue decimal.Decimal `json:"-"`
	Count   int             `json:"-"`
}

// HourlyPoint is one hour-of-day bucket within TodayReport.HourlyTrend.
type HourlyPoint struct {
	Hour     string          `json:"hour"`
	NetSales decimal.Decimal `json:"net_sales"`
}

// PaymentMethodBreakdown is one payment method's totals - reused by
// TodayReport, SalesSummary.ByPaymentMethod, and PaymentMethodsReport.
// Amount is the actual amount collected (Payment.Amount, tax-inclusive -
// this answers "how much came in via this channel", a different question
// from the tax-excluded NetSales revenue figure used elsewhere).
type PaymentMethodBreakdown struct {
	Method     string          `json:"method"`
	Amount     decimal.Decimal `json:"amount"`
	Count      int             `json:"count"`
	Percentage decimal.Decimal `json:"percentage"`
}

// ProductRevenue is one product's qty/revenue - reused by TodayReport,
// ProductSalesReport, and TopProductsReport. Revenue is
// SUM(SaleItem.LineTotal) (already net of that line's own discount,
// excludes tax) - not netted against Returns (see
// Service.GetProductSalesReport's doc).
type ProductRevenue struct {
	ProductID uint            `json:"product_id"`
	Name      string          `json:"name"`
	QtySold   int             `json:"qty_sold"`
	Revenue   decimal.Decimal `json:"revenue"`
}

// TodayReport backs `GET /reports/today` - bundles a full "Today" tab
// (headline totals, hourly trend, payment split, top 5 products) in one
// call so the frontend doesn't need four round trips for its default view.
type TodayReport struct {
	SalesTotals
	HourlyTrend    []HourlyPoint            `json:"hourly_trend"`
	PaymentMethods []PaymentMethodBreakdown `json:"payment_methods"`
	TopProducts    []ProductRevenue         `json:"top_products"`
}

// TrendPoint is one bucket within RevenueTrend.Points.
type TrendPoint struct {
	Period   string          `json:"period"`
	NetSales decimal.Decimal `json:"net_sales"`
}

// RevenueTrend backs `GET /reports/revenue-trend`.
type RevenueTrend struct {
	Granularity Granularity  `json:"granularity"`
	Points      []TrendPoint `json:"points"`
}

// SalesTrendPoint is one bucket within SalesTrend.Points.
type SalesTrendPoint struct {
	Period           string          `json:"period"`
	TransactionCount int             `json:"transaction_count"`
	NetSales         decimal.Decimal `json:"net_sales"`
}

// SalesTrend backs `GET /reports/sales-trend`.
type SalesTrend struct {
	Granularity Granularity       `json:"granularity"`
	Points      []SalesTrendPoint `json:"points"`
}

// BranchBreakdown is one branch's totals within SalesSummary.ByBranch.
type BranchBreakdown struct {
	BranchID   uint            `json:"branch_id"`
	BranchName string          `json:"branch_name"`
	NetSales   decimal.Decimal `json:"net_sales"`
}

// CategoryBreakdown is one category's totals within SalesSummary.ByCategory.
type CategoryBreakdown struct {
	CategoryID   uint            `json:"category_id"`
	CategoryName string          `json:"category_name"`
	NetSales     decimal.Decimal `json:"net_sales"`
}

// SalesSummary backs `GET /reports/sales-summary` - the confirmed spec's
// core breakdown: headline Net Sales plus the by-branch/by-payment-method/
// by-category views it requires.
type SalesSummary struct {
	SalesTotals
	ByBranch        []BranchBreakdown        `json:"by_branch"`
	ByPaymentMethod []PaymentMethodBreakdown `json:"by_payment_method"`
	ByCategory      []CategoryBreakdown      `json:"by_category"`
}

// ProfitAndLoss backs `GET /reports/profit-loss` - a real P&L statement:
//
//	Net Sales
//	− COGS            (cost of goods sold, from SaleItem.UnitCost snapshots)
//	= Gross Profit
//	− Expenses        (shift.Expense, already real per-branch data)
//	= Net Profit
//
// COGS relies on SaleItem.UnitCost having been snapshotted at sale time
// (see its own doc) - a sale item created before that existed reads as
// UnitCost 0, so COGS (and therefore margin) for older sales will
// understate cost until the catalog's cost basis has been established via
// a real goods receipt or a manual UpdateProduct edit.
type ProfitAndLoss struct {
	SalesTotals
	COGS        decimal.Decimal `json:"cogs"`
	GrossProfit decimal.Decimal `json:"gross_profit"`
	Expenses    decimal.Decimal `json:"expenses"`
	NetProfit   decimal.Decimal `json:"net_profit"`
}

// PaymentMethodsReport backs `GET /reports/payment-methods`.
type PaymentMethodsReport struct {
	Total   decimal.Decimal          `json:"total"`
	Methods []PaymentMethodBreakdown `json:"methods"`
}

// TransactionSummary is one row within TransactionsReport.Transactions -
// also the direct scan target for Repository.ListTransactions.
type TransactionSummary struct {
	ID          uuid.UUID       `json:"id"`
	BranchID    uint            `json:"branch_id"`
	StaffID     uint            `json:"staff_id"`
	Total       decimal.Decimal `json:"total"`
	Status      string          `json:"status"`
	CompletedAt *time.Time      `json:"completed_at"`
}

// TransactionsReport backs `GET /reports/transactions`.
type TransactionsReport struct {
	Transactions []TransactionSummary `json:"transactions"`
	Page         int                  `json:"page"`
	PageSize     int                  `json:"page_size"`
	TotalCount   int64                `json:"total_count"`
}

// ProductSalesReport backs `GET /reports/product-sales`.
type ProductSalesReport struct {
	Products []ProductRevenue `json:"products"`
}

// TopProductsReport backs `GET /reports/top-products`.
type TopProductsReport struct {
	Products []ProductRevenue `json:"products"`
}
