package sales

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// SaleStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type SaleStatus string

const (
	SaleStatusOpen      SaleStatus = "open"
	SaleStatusCompleted SaleStatus = "completed"
	SaleStatusVoided    SaleStatus = "voided"
	SaleStatusRefunded  SaleStatus = "refunded"
)

// PaymentMethod is a best-guess enum (ERD only specified "enum"; confirm real values).
type PaymentMethod string

const (
	PaymentMethodCash         PaymentMethod = "cash"
	PaymentMethodCard         PaymentMethod = "card"
	PaymentMethodMobileWallet PaymentMethod = "mobile_wallet"
	PaymentMethodStoreCredit  PaymentMethod = "store_credit"
	PaymentMethodOther        PaymentMethod = "other"
)

// Sale maps to the "Sales" table in the ERD.
type Sale struct {
	ID          uuid.UUID       `gorm:"primaryKey;type:uuid" json:"id"`
	OrgID       uint            `gorm:"index;not null" json:"org_id"`
	BranchID    uint            `gorm:"index;not null" json:"branch_id"`
	ShiftID     uint            `gorm:"index;not null" json:"shift_id"`
	StaffID     uint            `gorm:"index;not null" json:"staff_id"`
	DeviceID    uint            `gorm:"index;not null" json:"device_id"`
	CustomerID  *uint           `gorm:"index" json:"customer_id"`
	Subtotal    decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"subtotal"`
	Discount    decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"discount"`
	Tax         decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"tax"`
	Total       decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"total"`
	Status      SaleStatus      `gorm:"type:varchar(30);not null" json:"status"` // one of SaleStatus* constants below (TODO: confirm real values)
	CompletedAt *time.Time      `json:"completed_at"`
	SyncedAt    *time.Time      `json:"synced_at"`
}

// SaleItem maps to the "Sale_items" table in the ERD.
type SaleItem struct {
	ID            uint             `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID        uuid.UUID        `gorm:"type:uuid;index;not null" json:"sale_id"`
	ProductID     uint             `gorm:"index;not null" json:"product_id"`
	NameSnapshot  string           `gorm:"size:255;not null" json:"name_snapshot"`
	UnitPrice     decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"unit_price"`
	PriceOverride *decimal.Decimal `gorm:"type:decimal(10,2)" json:"price_override"`
	Qty           int              `gorm:"not null" json:"qty"`
	LineTotal     decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"line_total"`
	Discount      decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"discount"`
}

// Payment maps to the "Payments" table in the ERD.
type Payment struct {
	ID             uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID         uuid.UUID       `gorm:"type:uuid;index;not null" json:"sale_id"`
	Method         PaymentMethod   `gorm:"type:varchar(30);not null" json:"method"` // one of PaymentMethod* constants below (TODO: confirm real values)
	Amount         decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"amount"`
	AmountReceived decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"amount_received"`
	ChangeGiven    decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"change_given"`
}

// HeldSale maps to the "Held_sales" table in the ERD.
type HeldSale struct {
	ID          uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	BranchID    uint            `gorm:"index;not null" json:"branch_id"`
	StaffID     uint            `gorm:"index;not null" json:"staff_id"`
	CustomerRef *uint           `gorm:"index" json:"customer_ref"`
	Items       datatypes.JSON  `gorm:"type:jsonb;not null" json:"items"`
	Discount    decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"discount"`
	HeldAt      time.Time       `gorm:"not null" json:"held_at"`
}
