package platform

import "context"

// Interface defines the platform domain's use cases.
type Interface interface {
	GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error)
	UpdateReceiptSettings(ctx context.Context, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error)
	TestPrinter(ctx context.Context) (map[string]any, error)
}
