package platform

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error) {
	return s.repo.GetReceiptSettings(ctx)
}

func (s *Service) UpdateReceiptSettings(ctx context.Context, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error) {
	return s.repo.UpdateReceiptSettings(ctx, in)
}

func (s *Service) TestPrinter(ctx context.Context) (map[string]any, error) {
	return s.repo.TestPrinter(ctx)
}
