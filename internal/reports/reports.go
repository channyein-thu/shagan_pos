package reports

import "context"

// Interface defines the reports domain's use cases.
type Interface interface {
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

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) GetHomeSummary(ctx context.Context) (map[string]any, error) {
	return s.repo.GetHomeSummary(ctx)
}

func (s *Service) GetStockOverview(ctx context.Context) (map[string]any, error) {
	return s.repo.GetStockOverview(ctx)
}

func (s *Service) GetTodayReport(ctx context.Context) (map[string]any, error) {
	return s.repo.GetTodayReport(ctx)
}

func (s *Service) GetRevenueTrend(ctx context.Context) (map[string]any, error) {
	return s.repo.GetRevenueTrend(ctx)
}

func (s *Service) GetSalesSummary(ctx context.Context) (map[string]any, error) {
	return s.repo.GetSalesSummary(ctx)
}

func (s *Service) GetSalesTrend(ctx context.Context) (map[string]any, error) {
	return s.repo.GetSalesTrend(ctx)
}

func (s *Service) GetPaymentMethodsReport(ctx context.Context) (map[string]any, error) {
	return s.repo.GetPaymentMethodsReport(ctx)
}

func (s *Service) GetTransactionsReport(ctx context.Context) (map[string]any, error) {
	return s.repo.GetTransactionsReport(ctx)
}

func (s *Service) GetProductSalesReport(ctx context.Context) (map[string]any, error) {
	return s.repo.GetProductSalesReport(ctx)
}

func (s *Service) GetTopProducts(ctx context.Context) (map[string]any, error) {
	return s.repo.GetTopProducts(ctx)
}

func (s *Service) ExportReport(ctx context.Context) (map[string]any, error) {
	return s.repo.ExportReport(ctx)
}
