package platform

import (
	"context"

	"gorm.io/gorm"
)

// Repository defines the platform domain's persistence operations. Plain DB
// reads/writes only - validation, storage I/O, and the active-QR-code
// cap/duplicate-bank-name checks are the service's job (see Service).
// TestPrinter has no backing table, so it isn't here at all - the service
// handles it entirely in-memory.
type Repository interface {
	// GetReceiptSettings returns the single row matching orgID and branchID -
	// a nil branchID matches the org-wide default row (BranchID IS NULL).
	// common.NotFoundError if no such row exists yet.
	GetReceiptSettings(ctx context.Context, orgID uint, branchID *uint) (*ReceiptSetting, error)
	// UpsertReceiptSettings creates or replaces the row matching orgID and
	// in.BranchID.
	UpsertReceiptSettings(ctx context.Context, orgID uint, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error)

	// CreatePaymentQRCode backs Service.UploadPaymentQRCode's DB insert. db is
	// either the repository's normal connection or an in-flight transaction -
	// Service.UploadPaymentQRCode runs this and the storage upload inside one
	// db.Transaction, same reasoning as catalog.Repository.CreateProduct.
	CreatePaymentQRCode(db *gorm.DB, qr *PaymentQRCode) error
	// ListActivePaymentQRCodes backs Service.ListPaymentQRCodes and the
	// cap/duplicate-bank-name checks in Service.UploadPaymentQRCode.
	ListActivePaymentQRCodes(ctx context.Context, branchID uint) ([]PaymentQRCode, error)
	// GetPaymentQRCode backs Service.DeletePaymentQRCode's existence/
	// ownership check. common.NotFoundError for a QR code that exists but
	// belongs to a different branch, same as one that doesn't exist at all -
	// same not-found-not-forbidden reasoning as identity.GetBranch.
	GetPaymentQRCode(ctx context.Context, branchID uint, id uint) (*PaymentQRCode, error)
	// DeactivatePaymentQRCode sets IsActive false. Plain write - existence/
	// ownership was already confirmed by a prior GetPaymentQRCode call.
	DeactivatePaymentQRCode(db *gorm.DB, id uint) error
}
