package platform

import (
	"context"
	"io"

	"shagan_pos/internal/identity"
)

// BranchLookup is the one identity operation platform needs: confirming a
// client-supplied branch ID actually belongs to the caller's org before
// touching its receipt settings or payment QR codes. identity.Repository
// already satisfies this signature - no adapter needed, same reasoning as
// catalog.BranchLookup.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
}

// MaxActivePaymentQRCodesPerBranch is the confirmed cap - a branch displays
// at most this many bank QR codes for customers to scan, one per bank.
// Deactivating one (DeletePaymentQRCode) frees a slot.
const MaxActivePaymentQRCodesPerBranch = 5

// Interface defines the platform domain's use cases.
type Interface interface {
	// GetReceiptSettings returns branchID's override if one exists, else
	// falls back to orgID's org-wide default. A nil branchID returns the
	// org-wide default directly.
	GetReceiptSettings(ctx context.Context, orgID uint, branchID *uint) (*ReceiptSetting, error)
	// UpdateReceiptSettings upserts the org-wide default (in.BranchID nil) or
	// a branch-specific override (in.BranchID set).
	UpdateReceiptSettings(ctx context.Context, orgID uint, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error)
	// TestPrinter is a pure simulation - no real printer integration exists,
	// so this just confirms branchID belongs to orgID and renders a canned
	// test-receipt payload. Nothing is persisted.
	TestPrinter(ctx context.Context, orgID, branchID uint) (map[string]any, error)
	// UploadPaymentQRCode validates branchID belongs to orgID, rejects a
	// non-image file, rejects if branchID already has
	// MaxActivePaymentQRCodesPerBranch active QR codes, and rejects a
	// duplicate active bankName for the same branch.
	UploadPaymentQRCode(ctx context.Context, orgID, branchID uint, bankName string, file io.ReadSeeker, size int64, contentType string) (*PaymentQRCodeResult, error)
	// ListPaymentQRCodes returns branchID's active QR codes only.
	ListPaymentQRCodes(ctx context.Context, orgID, branchID uint) ([]PaymentQRCodeResult, error)
	// DeletePaymentQRCode soft-deletes - sets IsActive false and removes the
	// image from storage, freeing a cap slot while keeping the row for
	// history. common.NotFoundError for a QR code that's already inactive,
	// doesn't exist, or belongs to a different branch.
	DeletePaymentQRCode(ctx context.Context, orgID, branchID, qrID uint) error
}
