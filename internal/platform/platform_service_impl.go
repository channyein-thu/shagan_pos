package platform

import (
	"context"
	"io"
)

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

func (s *Service) UploadPaymentQRCode(ctx context.Context, branchID uint, provider string, file io.Reader, size int64, contentType string) (*PaymentQRCode, error) {
	return s.repo.UploadPaymentQRCode(ctx, branchID, provider, file, size, contentType)
}

func (s *Service) ListPaymentQRCodes(ctx context.Context, branchID uint) ([]PaymentQRCode, error) {
	return s.repo.ListPaymentQRCodes(ctx, branchID)
}

func (s *Service) DeletePaymentQRCode(ctx context.Context, branchID uint, qrID uint) error {
	return s.repo.DeletePaymentQRCode(ctx, branchID, qrID)
}
