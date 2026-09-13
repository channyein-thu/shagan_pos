package platform

import (
	"context"
	"io"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/storage"
)

type RepositoryImpl struct {
	db      *gorm.DB
	storage storage.Storage
}

func NewRepository(db *gorm.DB, store storage.Storage) Repository {
	return &RepositoryImpl{db: db, storage: store}
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

// UploadPaymentQRCode backs `POST /branches/:id/payment-qr-codes`.
// TODO: upload `file` to r.storage under a key like
// fmt.Sprintf("qr-codes/branch-%d/%s", branchID, provider), then insert a
// PaymentQRCode row pointing at that key.
func (r *RepositoryImpl) UploadPaymentQRCode(ctx context.Context, branchID uint, provider string, file io.Reader, size int64, contentType string) (*PaymentQRCode, error) {
	return nil, common.ErrNotImplemented
}

// ListPaymentQRCodes backs `GET /branches/:id/payment-qr-codes`.
func (r *RepositoryImpl) ListPaymentQRCodes(ctx context.Context, branchID uint) ([]PaymentQRCode, error) {
	return nil, common.ErrNotImplemented
}

// DeletePaymentQRCode backs `DELETE /branches/:id/payment-qr-codes/:qrId`.
// TODO: also delete the object from r.storage, not just the DB row.
func (r *RepositoryImpl) DeletePaymentQRCode(ctx context.Context, branchID uint, qrID uint) error {
	return common.ErrNotImplemented
}
