package platform

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers JPEG decoding with image.DecodeConfig
	_ "image/png"  // registers PNG decoding with image.DecodeConfig
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/storage"
)

// DefaultQRImageURLTTL is how long a presigned QR-code image URL stays valid
// for - same reasoning/value as catalog.DefaultImageURLTTL.
const DefaultQRImageURLTTL = 15 * time.Minute

type Service struct {
	repo     Repository
	branches BranchLookup
	db       common.Transactioner
	storage  storage.Storage
}

func NewService(repo Repository, branches BranchLookup, db common.Transactioner, store storage.Storage) *Service {
	return &Service{repo: repo, branches: branches, db: db, storage: store}
}

var _ Interface = (*Service)(nil)

// isNotFound reports whether err is a common.RestError with a 404 status -
// used below to detect "no branch-specific override exists yet" without a
// dedicated repository method.
func isNotFound(err error) bool {
	var restErr common.RestError
	return errors.As(err, &restErr) && restErr.Status == http.StatusNotFound
}

func (s *Service) GetReceiptSettings(ctx context.Context, orgID uint, branchID *uint) (*ReceiptSetting, error) {
	if branchID != nil {
		if _, err := s.branches.GetBranch(ctx, orgID, *branchID); err != nil {
			return nil, err
		}
		setting, err := s.repo.GetReceiptSettings(ctx, orgID, branchID)
		if err == nil {
			return setting, nil
		}
		if !isNotFound(err) {
			return nil, err
		}
		// no branch-specific override yet - fall through to the org default.
	}
	return s.repo.GetReceiptSettings(ctx, orgID, nil)
}

func (s *Service) UpdateReceiptSettings(ctx context.Context, orgID uint, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error) {
	if in.BranchID != nil {
		if _, err := s.branches.GetBranch(ctx, orgID, *in.BranchID); err != nil {
			return nil, err
		}
	}
	return s.repo.UpsertReceiptSettings(ctx, orgID, in)
}

func (s *Service) TestPrinter(ctx context.Context, orgID, branchID uint) (map[string]any, error) {
	branch, err := s.branches.GetBranch(ctx, orgID, branchID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":     "ok",
		"branch_id":  branch.ID,
		"printed_at": time.Now().UTC(),
		"lines": []string{
			"*** TEST PRINT ***",
			branch.Name,
			"This is a test receipt.",
			"If you can read this, the printer is working.",
		},
	}, nil
}

func (s *Service) UploadPaymentQRCode(ctx context.Context, orgID, branchID uint, bankName string, file io.ReadSeeker, size int64, contentType string) (*PaymentQRCodeResult, error) {
	if _, err := s.branches.GetBranch(ctx, orgID, branchID); err != nil {
		return nil, err
	}
	if bankName == "" {
		return nil, common.BadRequestError("bank_name is required")
	}

	active, err := s.repo.ListActivePaymentQRCodes(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if len(active) >= MaxActivePaymentQRCodesPerBranch {
		return nil, common.BadRequestError(fmt.Sprintf("branch already has %d active payment QR codes - delete one first", MaxActivePaymentQRCodesPerBranch))
	}
	for _, qr := range active {
		if qr.BankName == bankName {
			return nil, common.ConflictError("branch already has an active payment QR code for this bank")
		}
	}

	// image.DecodeConfig only reads the header (not the full pixel data) to
	// confirm this is a real image, but it still consumes bytes from file -
	// rewind before the full content gets uploaded below.
	if _, _, err := image.DecodeConfig(file); err != nil {
		return nil, common.BadRequestError("uploaded file is not a valid image")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, common.SystemError("failed to rewind uploaded image")
	}

	// Keyed by a random id rather than the row's own id (unlike
	// catalog.Service.CreateProduct) - that lets the row get created with
	// its final StorageKey in one write, instead of insert-then-update.
	key := fmt.Sprintf("payment-qr-codes/branch-%d/%s", branchID, uuid.New().String())

	var result *PaymentQRCode
	err = s.db.Transaction(func(tx *gorm.DB) error {
		qr := PaymentQRCode{
			BranchID:   branchID,
			BankName:   bankName,
			StorageKey: key,
			IsActive:   true,
		}
		if err := s.repo.CreatePaymentQRCode(tx, &qr); err != nil {
			return err
		}
		if err := s.storage.Upload(ctx, key, file, size, contentType); err != nil {
			return common.SystemError("failed to upload QR code image")
		}
		result = &qr
		return nil
	})
	if err != nil {
		return nil, err
	}

	url, err := s.storage.PresignedURL(ctx, result.StorageKey, DefaultQRImageURLTTL)
	if err != nil {
		return nil, err
	}
	return &PaymentQRCodeResult{PaymentQRCode: *result, ImageURL: url}, nil
}

func (s *Service) ListPaymentQRCodes(ctx context.Context, orgID, branchID uint) ([]PaymentQRCodeResult, error) {
	if _, err := s.branches.GetBranch(ctx, orgID, branchID); err != nil {
		return nil, err
	}
	codes, err := s.repo.ListActivePaymentQRCodes(ctx, branchID)
	if err != nil {
		return nil, err
	}
	results := make([]PaymentQRCodeResult, len(codes))
	for i, qr := range codes {
		url, err := s.storage.PresignedURL(ctx, qr.StorageKey, DefaultQRImageURLTTL)
		if err != nil {
			return nil, err
		}
		results[i] = PaymentQRCodeResult{PaymentQRCode: qr, ImageURL: url}
	}
	return results, nil
}

func (s *Service) DeletePaymentQRCode(ctx context.Context, orgID, branchID, qrID uint) error {
	if _, err := s.branches.GetBranch(ctx, orgID, branchID); err != nil {
		return err
	}
	qr, err := s.repo.GetPaymentQRCode(ctx, branchID, qrID)
	if err != nil {
		return err
	}
	if !qr.IsActive {
		return common.NotFoundError("payment QR code not found")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.DeactivatePaymentQRCode(tx, qrID); err != nil {
			return err
		}
		_ = s.storage.Delete(ctx, qr.StorageKey)
		return nil
	})
}
