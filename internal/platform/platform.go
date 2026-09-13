package platform

import "context"

// Interface defines the platform domain's use cases.
type Interface interface {
	GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error)
	UpdateReceiptSettings(ctx context.Context, in ReceiptSetting) (*ReceiptSetting, error)
	TestPrinter(ctx context.Context) (map[string]any, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error) {
	return s.repo.GetReceiptSettings(ctx)
}

func (s *Service) UpdateReceiptSettings(ctx context.Context, in ReceiptSetting) (*ReceiptSetting, error) {
	return s.repo.UpdateReceiptSettings(ctx, in)
}

func (s *Service) TestPrinter(ctx context.Context) (map[string]any, error) {
	return s.repo.TestPrinter(ctx)
}
