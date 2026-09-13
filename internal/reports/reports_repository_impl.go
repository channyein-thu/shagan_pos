package reports

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// GetHomeSummary backs `GET /reports/home-summary`.
func (r *RepositoryImpl) GetHomeSummary(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetStockOverview backs `GET /reports/stock-overview`.
func (r *RepositoryImpl) GetStockOverview(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetTodayReport backs `GET /reports/today`.
func (r *RepositoryImpl) GetTodayReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetRevenueTrend backs `GET /reports/revenue-trend`.
func (r *RepositoryImpl) GetRevenueTrend(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetSalesSummary backs `GET /reports/sales-summary`.
func (r *RepositoryImpl) GetSalesSummary(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetSalesTrend backs `GET /reports/sales-trend`.
func (r *RepositoryImpl) GetSalesTrend(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetPaymentMethodsReport backs `GET /reports/payment-methods`.
func (r *RepositoryImpl) GetPaymentMethodsReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetTransactionsReport backs `GET /reports/transactions`.
func (r *RepositoryImpl) GetTransactionsReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetProductSalesReport backs `GET /reports/product-sales`.
func (r *RepositoryImpl) GetProductSalesReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// GetTopProducts backs `GET /reports/top-products`.
func (r *RepositoryImpl) GetTopProducts(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ExportReport backs `POST /reports/export`. DEFER per the cut list
func (r *RepositoryImpl) ExportReport(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}
