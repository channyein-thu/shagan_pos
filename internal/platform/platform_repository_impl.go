package platform

import (
	"context"
	"errors"

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

// GetReceiptSettings backs Service.GetReceiptSettings' lookups.
func (r *RepositoryImpl) GetReceiptSettings(ctx context.Context, orgID uint, branchID *uint) (*ReceiptSetting, error) {
	var setting ReceiptSetting
	query := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if branchID != nil {
		query = query.Where("branch_id = ?", *branchID)
	} else {
		query = query.Where("branch_id IS NULL")
	}
	if err := query.First(&setting).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("receipt settings not found")
		}
		return nil, err
	}
	return &setting, nil
}

// UpsertReceiptSettings backs Service.UpdateReceiptSettings.
func (r *RepositoryImpl) UpsertReceiptSettings(ctx context.Context, orgID uint, in UpdateReceiptSettingsRequest) (*ReceiptSetting, error) {
	var setting ReceiptSetting
	query := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if in.BranchID != nil {
		query = query.Where("branch_id = ?", *in.BranchID)
	} else {
		query = query.Where("branch_id IS NULL")
	}

	err := query.First(&setting).Error
	switch {
	case err == nil:
		setting.ShopName = in.ShopName
		setting.Address = in.Address
		setting.Phone = in.Phone
		setting.ThankYou = in.ThankYou
		if err := r.db.WithContext(ctx).Save(&setting).Error; err != nil {
			return nil, err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		setting = ReceiptSetting{
			OrgID:    orgID,
			BranchID: in.BranchID,
			ShopName: in.ShopName,
			Address:  in.Address,
			Phone:    in.Phone,
			ThankYou: in.ThankYou,
			IsGlobal: in.BranchID == nil,
		}
		if err := r.db.WithContext(ctx).Create(&setting).Error; err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return &setting, nil
}

// CreatePaymentQRCode backs Service.UploadPaymentQRCode's first step. Plain
// insert - GORM sets the row's ID on the pointer it's given.
func (r *RepositoryImpl) CreatePaymentQRCode(db *gorm.DB, qr *PaymentQRCode) error {
	return db.Create(qr).Error
}

// ListActivePaymentQRCodes backs Service.ListPaymentQRCodes and the
// cap/duplicate-bank-name checks in Service.UploadPaymentQRCode.
func (r *RepositoryImpl) ListActivePaymentQRCodes(ctx context.Context, branchID uint) ([]PaymentQRCode, error) {
	var codes []PaymentQRCode
	if err := r.db.WithContext(ctx).
		Where("branch_id = ? AND is_active = ?", branchID, true).
		Order("created_at").
		Find(&codes).Error; err != nil {
		return nil, err
	}
	return codes, nil
}

// GetPaymentQRCode backs Service.DeletePaymentQRCode's existence/ownership check.
func (r *RepositoryImpl) GetPaymentQRCode(ctx context.Context, branchID uint, id uint) (*PaymentQRCode, error) {
	var qr PaymentQRCode
	err := r.db.WithContext(ctx).Where("id = ? AND branch_id = ?", id, branchID).First(&qr).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("payment QR code not found")
		}
		return nil, err
	}
	return &qr, nil
}

// DeactivatePaymentQRCode backs Service.DeletePaymentQRCode's second step.
func (r *RepositoryImpl) DeactivatePaymentQRCode(db *gorm.DB, id uint) error {
	return db.Model(&PaymentQRCode{}).Where("id = ?", id).Update("is_active", false).Error
}
