package platform

import (
	"context"
	"io"
)

// Repository defines the platform domain's persistence operations.
type Repository interface {
	GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error)
	UpdateReceiptSettings(ctx context.Context, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error)
	TestPrinter(ctx context.Context) (map[string]any, error)

	// UploadPaymentQRCode uploads the QR image to object storage and records
	// it against branchID. contentType/size describe the uploaded file.
	UploadPaymentQRCode(ctx context.Context, branchID uint, provider string, file io.Reader, size int64, contentType string) (*PaymentQRCode, error)
	ListPaymentQRCodes(ctx context.Context, branchID uint) ([]PaymentQRCode, error)
	DeletePaymentQRCode(ctx context.Context, branchID uint, qrID uint) error
}
