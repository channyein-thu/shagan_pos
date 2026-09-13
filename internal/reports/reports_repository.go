package reports

import "context"

// Repository defines the reports domain's persistence operations.
type Repository interface {
	GetHomeSummary(ctx context.Context) (map[string]any, error)
	GetStockOverview(ctx context.Context) (map[string]any, error)
	GetTodayReport(ctx context.Context) (map[string]any, error)
	GetRevenueTrend(ctx context.Context) (map[string]any, error)
	GetSalesSummary(ctx context.Context) (map[string]any, error)
	GetSalesTrend(ctx context.Context) (map[string]any, error)
	GetPaymentMethodsReport(ctx context.Context) (map[string]any, error)
	GetTransactionsReport(ctx context.Context) (map[string]any, error)
	GetProductSalesReport(ctx context.Context) (map[string]any, error)
	GetTopProducts(ctx context.Context) (map[string]any, error)
	ExportReport(ctx context.Context) (map[string]any, error)
}
