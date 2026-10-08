package reports

import (
	"context"
	"time"

	"shagan_pos/internal/identity"
)

// BranchLookup is what reports needs from identity: resolving an owner's
// optional branch filter and enumerating an org's own branches - needed
// only for GetProfitAndLoss's Expenses figure (shift.Expense carries no
// OrgID of its own, unlike Sale, so every other Reports query gets away
// with a plain org_id filter and never needed this). identity.Repository
// already satisfies this signature - no adapter needed, same reasoning as
// inventory.BranchLookup.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
	ListBranches(ctx context.Context, orgID uint) ([]identity.Branch, error)
	// GetOrganization supplies the org's Timezone, which defines its
	// calendar day for every date-based figure here.
	GetOrganization(ctx context.Context, id uint) (*identity.Organization, error)
}

// Interface defines the reports domain's use cases. Every method (other
// than ExportReport, deferred - see its own doc) is read-only and scoped to
// orgID; branchID narrows to one branch when set - an owner/service_center
// caller may omit it for an org-wide view or pass any branch in their own
// org, while a branch-scoped caller's own branch always wins instead
// (enforced by the handler, same convention as inventory.Service). from/to
// are optional - nil means "default to the last 30 days", applied inside
// the service, never the handler (see GetSalesSummary's doc for why that's
// a business rule, not request parsing).
type Interface interface {
	// GetHomeSummary is the dashboard's header cards - always today, no
	// date range. Net Sales uses the same Gross-Discounts-Returns formula
	// as GetSalesSummary; LowStockCount is a same-domain convenience so the
	// frontend doesn't need a second call to StockOverview for one number.
	GetHomeSummary(ctx context.Context, orgID uint, branchID *uint) (*HomeSummary, error)
	// GetStockOverview is a dashboard widget - always current (stock levels
	// have no historical dimension), no date range.
	GetStockOverview(ctx context.Context, orgID uint, branchID *uint) (*StockOverview, error)
	// GetTodayReport bundles a full "Today" tab - headline totals, an
	// hourly trend, a payment-method split, and the top 5 products, all
	// pinned to today - in one call, so the frontend's default Reports view
	// doesn't need four round trips.
	GetTodayReport(ctx context.Context, orgID uint, branchID *uint) (*TodayReport, error)
	// GetRevenueTrend is Net Sales bucketed by granularity (default daily)
	// over from/to (default: the last 30 days). Deliberately does not net
	// out Returns per-bucket - Returns are recorded against their own
	// CreatedAt, which can fall in a different bucket than the original
	// sale, and bucket-level netting would require attributing each return
	// back to the bucket its original sale occurred in; not worth the
	// complexity while Returns is unbuilt (every real bucket has zero
	// returns today regardless). Only the aggregate endpoints
	// (GetHomeSummary/GetTodayReport/GetSalesSummary) net out Returns.
	GetRevenueTrend(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, granularity Granularity) (*RevenueTrend, error)
	// GetSalesSummary is the confirmed spec's core breakdown: headline Net
	// Sales (Gross-Discounts-Returns, tax excluded, voided sales never
	// counted - see SalesTotals's doc) plus by-branch/by-payment-method/
	// by-category views, over from/to (default: the last 30 days).
	GetSalesSummary(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time) (*SalesSummary, error)
	// GetSalesTrend is transaction count plus Net Sales bucketed by
	// granularity (default daily) over from/to (default: the last 30 days) -
	// same not-netting-Returns-per-bucket reasoning as GetRevenueTrend.
	GetSalesTrend(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, granularity Granularity) (*SalesTrend, error)
	// GetPaymentMethodsReport is each payment method's share of amount
	// actually collected (tax-inclusive - see PaymentMethodBreakdown's own
	// doc), over from/to (default: the last 30 days).
	GetPaymentMethodsReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time) (*PaymentMethodsReport, error)
	// GetTransactionsReport is a paginated raw sale list over from/to
	// (default: the last 30 days). page defaults to 1, pageSize to 20
	// (capped at 100) when either is <= 0.
	GetTransactionsReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, page, pageSize int) (*TransactionsReport, error)
	// GetProductSalesReport is every product's qty/revenue over from/to
	// (default: the last 30 days), optionally narrowed to one category.
	// Revenue is not netted against Returns - see ProductRevenue's own doc.
	GetProductSalesReport(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, categoryID *uint) (*ProductSalesReport, error)
	// GetTopProducts is the top N products ranked by revenue (default
	// limit 10) over from/to (default: the last 30 days) - same
	// not-netted-against-Returns reasoning as GetProductSalesReport.
	GetTopProducts(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time, limit *int) (*TopProductsReport, error)
	// GetProfitAndLoss is a real P&L statement (Net Sales − COGS = Gross
	// Profit; Gross Profit − Expenses = Net Profit) over from/to (default:
	// the last 30 days) - see ProfitAndLoss's own doc for COGS's
	// reliability caveat on sales predating cost tracking.
	GetProfitAndLoss(ctx context.Context, orgID uint, branchID *uint, from, to *time.Time) (*ProfitAndLoss, error)
	// ExportReport stays deferred - out of the confirmed v1 spec (see
	// docs/WORKFLOWS.md Section 11), matching the existing stub's own
	// DEFER comment.
	ExportReport(ctx context.Context) (map[string]any, error)
}
