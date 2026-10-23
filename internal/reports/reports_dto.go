package reports

import (
	"encoding/json"
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
// - Discounts - Returns: what the shop took. Prices contain their tax, so for
// a sale made since tax became a rate inside the price that figure INCLUDES the
// tax (customers pay exactly the price); TaxCollected is the tax inside it and
// NetSalesExclTax is NetSales without it. A Voided sale never enters GrossSales
// at all (a void erases the whole transaction, per docs/WORKFLOWS.md's Void
// rule) rather than being counted and subtracted back out.
//
// TaxCollected = the tax of the period's sales - the tax inside what returns
// gave back + the tax difference of exchanges (what the new goods carry minus
// what the returned goods carried). A sale from before tax was inside the price
// had its tax added ON TOP of its total, so its subtotal - discount never
// contained it: it counts as tax collected but is not taken out of NetSales a
// second time in NetSalesExclTax.
type SalesTotals struct {
	GrossSales       decimal.Decimal `json:"gross_sales"`
	Discounts        decimal.Decimal `json:"discounts"`
	Returns          decimal.Decimal `json:"returns"`
	NetSales         decimal.Decimal `json:"net_sales"`
	TaxCollected     decimal.Decimal `json:"tax_collected"`
	NetSalesExclTax  decimal.Decimal `json:"net_sales_excl_tax"`
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
// from the NetSales revenue figure used elsewhere).
type PaymentMethodBreakdown struct {
	Method     string          `json:"method"`
	Amount     decimal.Decimal `json:"amount"`
	Count      int             `json:"count"`
	Percentage decimal.Decimal `json:"percentage"`
}

// ProductRevenue is one product's qty/revenue - reused by TodayReport,
// ProductSalesReport, and TopProductsReport. Revenue is
// SUM(SaleItem.LineTotal) (already net of that line's own discount; for a
// sale since tax became inclusive that includes the tax) - not netted against Returns (see
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

// WithoutCost is the statement as JSON fields minus everything that reveals
// what goods cost: COGS, and gross and net profit (either would give COGS away
// by subtraction). It is what a caller without view_item_cost gets back from
// `GET /reports/profit-loss`; Net Sales, discounts, returns and expenses stay.
func (p ProfitAndLoss) WithoutCost() map[string]any {
	raw, err := json.Marshal(p)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	delete(out, "cogs")
	delete(out, "gross_profit")
	delete(out, "net_profit")
	return out
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
	// PaymentMethods is every distinct method used on the sale, sorted
	// ("cash", "qr", or both for a split payment) - never null. Filled in by
	// the service from Repository.PaymentMethodsBySale, not scanned.
	PaymentMethods []string `json:"payment_methods" gorm:"-"`
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

// DiscountsReport backs `GET /reports/discounts`: how much was given away over
// a period, split by where the discount came from. Catalog discounts are the
// Owner's own price decisions (automatic); manual ones are custom discounts a
// seller asked for and an approver approved at the till - who asked and who
// approved are the two lists. Lines sold before the discount was split by
// source carry only their old combined discount and aren't in these totals.
// Lists are never null.
type DiscountsReport struct {
	CatalogTotal decimal.Decimal `json:"catalog_total"`
	ManualTotal  decimal.Decimal `json:"manual_total"`
	// ManualCount is how many sale lines carry a manual discount.
	ManualCount int `json:"manual_count"`
	// BySeller groups manual discounts by the staff member who asked for them,
	// ByApprover by the one who approved them (the same person when someone
	// approves their own).
	BySeller   []DiscountStaffRow `json:"by_seller"`
	ByApprover []DiscountStaffRow `json:"by_approver"`
	// CancelledCount is approvals that were granted but never spent: removed by
	// the till, or swept when their shift closed.
	CancelledCount int `json:"cancelled_count"`
}

// DiscountStaffRow is one staff member's manual discounts in a DiscountsReport.
type DiscountStaffRow struct {
	StaffID     uint            `json:"staff_id"`
	Name        string          `json:"name"`
	ManualTotal decimal.Decimal `json:"manual_total"`
	Count       int             `json:"count"`
}
