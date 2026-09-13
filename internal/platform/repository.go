package platform

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// GetReceiptSettings backs `GET /receipt-settings`. Needed for the edit form's pre-fill / live preview
func (r *Repository) GetReceiptSettings(ctx context.Context) (*ReceiptSetting, error) {
	return nil, common.ErrNotImplemented
}

// UpdateReceiptSettings backs `PUT /receipt-settings`.
func (r *Repository) UpdateReceiptSettings(ctx context.Context, in ReceiptSetting) (*ReceiptSetting, error) {
	return nil, common.ErrNotImplemented
}

// TestPrinter backs `POST /printers/test`. No table; renders a test payload
func (r *Repository) TestPrinter(ctx context.Context) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}
