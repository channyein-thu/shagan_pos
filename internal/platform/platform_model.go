package platform

import "time"

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// Translation maps to the "translations_locales" table in the ERD.
type Translation struct {
	Locale string `gorm:"primaryKey;size:10" json:"locale"`
	Key    string `gorm:"primaryKey;size:255" json:"key"`
	Value  string `gorm:"not null" json:"value"`
}

// ReceiptSetting maps to the "receipt_settings" table in the ERD.
type ReceiptSetting struct {
	ID       uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID    uint   `gorm:"index;not null" json:"org_id"`
	BranchID *uint  `gorm:"index" json:"branch_id"`
	ShopName string `gorm:"size:255;not null" json:"shop_name"`
	Address  string `gorm:"size:500;not null" json:"address"`
	Phone    string `gorm:"size:25;not null" json:"phone"`
	ThankYou string `gorm:"not null" json:"thank_you"`
	IsGlobal bool   `gorm:"not null" json:"is_global"`
}

func (Translation) TableName() string { return "translations_locales" }

// PaymentQRCode is a static QR code image (e.g. a KBZPay/WavePay/bank QR
// sticker) a branch displays for customers to scan and pay. Not part of the
// original ERD - added to back QR-based payments. Provider is a free-form
// label (e.g. "kbzpay", "wavepay") since the real set of providers isn't
// fixed yet. The actual image bytes live in object storage (see
// internal/storage); StorageKey just points at them.
type PaymentQRCode struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	BranchID   uint      `gorm:"index;not null" json:"branch_id"`
	Provider   string    `gorm:"size:100;not null" json:"provider"`
	StorageKey string    `gorm:"size:500;not null" json:"storage_key"`
	IsActive   bool      `gorm:"not null" json:"is_active"`
	CreatedAt  time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
}

func (PaymentQRCode) TableName() string { return "payment_qr_codes" }
