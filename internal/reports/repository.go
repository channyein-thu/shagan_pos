package reports

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// GetHomeSummary backs `GET /reports/home-summary`.
func (r *Repository) GetHomeSummary(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetStockOverview backs `GET /reports/stock-overview`.
func (r *Repository) GetStockOverview(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetTodayReport backs `GET /reports/today`.
func (r *Repository) GetTodayReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetRevenueTrend backs `GET /reports/revenue-trend`.
func (r *Repository) GetRevenueTrend(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetSalesSummary backs `GET /reports/sales-summary`.
func (r *Repository) GetSalesSummary(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetSalesTrend backs `GET /reports/sales-trend`.
func (r *Repository) GetSalesTrend(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetPaymentMethodsReport backs `GET /reports/payment-methods`.
func (r *Repository) GetPaymentMethodsReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetTransactionsReport backs `GET /reports/transactions`.
func (r *Repository) GetTransactionsReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetProductSalesReport backs `GET /reports/product-sales`.
func (r *Repository) GetProductSalesReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetTopProducts backs `GET /reports/top-products`.
func (r *Repository) GetTopProducts(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ExportReport backs `POST /reports/export`. DEFER per the cut list
func (r *Repository) ExportReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}
