package platform

import "context"

// Repository defines the platform domain's persistence operations.
type Repository interface {
	GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error)
	UpdateReceiptSettings(ctx context.Context, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error)
	TestPrinter(ctx context.Context) (map[string]any, error)
}
