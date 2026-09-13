package platform

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

// GetReceiptSettings backs `GET /receipt-settings`. Needed for the edit form's pre-fill / live preview
func (r *RepositoryImpl) GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error) {
	return nil, common.ErrNotImplemented
}

// UpdateReceiptSettings backs `PUT /receipt-settings`.
func (r *RepositoryImpl) UpdateReceiptSettings(ctx context.Context, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error) {
	return nil, common.ErrNotImplemented
}

// TestPrinter backs `POST /printers/test`. No table; renders a test payload
func (r *RepositoryImpl) TestPrinter(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}
