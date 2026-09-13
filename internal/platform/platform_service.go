package platform

import (
	"context"
	"io"
)

// Interface defines the platform domain's use cases.
type Interface interface {
	GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error)
	UpdateReceiptSettings(ctx context.Context, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error)
	TestPrinter(ctx context.Context) (map[string]any, error)
	UploadPaymentQRCode(ctx context.Context, branchID uint, provider string, file io.Reader, size int64, contentType string) (*PaymentQRCode, error)
	ListPaymentQRCodes(ctx context.Context, branchID uint) ([]PaymentQRCode, error)
	DeletePaymentQRCode(ctx context.Context, branchID uint, qrID uint) error
}
