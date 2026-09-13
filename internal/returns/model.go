package returns

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// VoidReason is a best-guess enum (ERD only specified "enum"; confirm real values).
type VoidReason string

const (
	VoidReasonCustomerRequest VoidReason = "customer_request"
	VoidReasonPriceError      VoidReason = "price_error"
	VoidReasonItemError       VoidReason = "item_error"
	VoidReasonStaffError      VoidReason = "staff_error"
	VoidReasonOther           VoidReason = "other"
)

// ReturnReasonCode is a best-guess enum (ERD only specified "enum"; confirm real values).
type ReturnReasonCode string

const (
	ReturnReasonCodeDefective   ReturnReasonCode = "defective"
	ReturnReasonCodeWrongItem   ReturnReasonCode = "wrong_item"
	ReturnReasonCodeChangedMind ReturnReasonCode = "changed_mind"
	ReturnReasonCodeOther       ReturnReasonCode = "other"
)

// RefundMethod is a best-guess enum (ERD only specified "enum"; confirm real values).
type RefundMethod string

const (
	RefundMethodCash            RefundMethod = "cash"
	RefundMethodCard            RefundMethod = "card"
	RefundMethodStoreCredit     RefundMethod = "store_credit"
	RefundMethodOriginalPayment RefundMethod = "original_payment"
)

// ItemCondition is a best-guess enum (ERD only specified "enum"; confirm real values).
type ItemCondition string

const (
	ItemConditionSellable  ItemCondition = "sellable"
	ItemConditionDamaged   ItemCondition = "damaged"
	ItemConditionOpened    ItemCondition = "opened"
	ItemConditionDefective ItemCondition = "defective"
)

// Direction is a best-guess enum (ERD only specified "enum"; confirm real values).
type Direction string

const (
	DirectionOut Direction = "out"
	DirectionIn  Direction = "in"
)

// Void maps to the "Voids" table in the ERD.
type Void struct {
	ID          uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID      uuid.UUID  `gorm:"type:uuid;index;not null" json:"sale_id"`
	SaleItemID  *uint      `gorm:"index" json:"sale_item_id"`
	Qty         int        `gorm:"not null" json:"qty"`
	Reason      VoidReason `gorm:"type:varchar(30);not null" json:"reason"` // one of VoidReason* constants below (TODO: confirm real values)
	Explanation string     `gorm:"not null" json:"explanation"`
	ApprovedBy  uint       `gorm:"index;not null" json:"approved_by"`
	CreatedAt   time.Time  `gorm:"autoCreateTime;not null" json:"created_at"`
}

// Return maps to the "Returns" table in the ERD.
type Return struct {
	ID           uint             `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID       uuid.UUID        `gorm:"type:uuid;index;not null" json:"sale_id"`
	ReasonCode   ReturnReasonCode `gorm:"type:varchar(30);not null" json:"reason_code"`   // one of ReturnReasonCode* constants below (TODO: confirm real values)
	RefundMethod RefundMethod     `gorm:"type:varchar(30);not null" json:"refund_method"` // one of RefundMethod* constants below (TODO: confirm real values)
	RefundTotal  decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"refund_total"`
	ApprovedBy   uint             `gorm:"index;not null" json:"approved_by"`
	CreatedAt    time.Time        `gorm:"autoCreateTime;not null" json:"created_at"`
}

// ReturnItem maps to the "Return_items" table in the ERD.
type ReturnItem struct {
	ID         uint          `gorm:"primaryKey;autoIncrement" json:"id"`
	ReturnID   uint          `gorm:"index;not null" json:"return_id"`
	SaleItemID uint          `gorm:"index;not null" json:"sale_item_id"`
	Qty        int           `gorm:"not null" json:"qty"`
	Condition  ItemCondition `gorm:"type:varchar(30);not null" json:"condition"` // one of ItemCondition* constants below (TODO: confirm real values)
	Restocked  bool          `gorm:"not null" json:"restocked"`
}

// Exchange maps to the "Exchanges" table in the ERD.
type Exchange struct {
	ID            uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID        uuid.UUID       `gorm:"type:uuid;index;not null" json:"sale_id"`
	NetDifference decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"net_difference"`
	ApprovedBy    uint            `gorm:"index;not null" json:"approved_by"`
	CreatedAt     time.Time       `gorm:"autoCreateTime;not null" json:"created_at"`
}

// ExchangeItem maps to the "Exchange_items" table in the ERD.
type ExchangeItem struct {
	ID         uint            `gorm:"primaryKey;autoIncrement" json:"id"`
	ExchangeID uint            `gorm:"index;not null" json:"exchange_id"`
	Direction  Direction       `gorm:"type:varchar(30);not null" json:"direction"` // one of Direction* constants below (TODO: confirm real values)
	SaleItemID *uint           `gorm:"index" json:"sale_item_id"`
	ProductID  *uint           `gorm:"index" json:"product_id"`
	Qty        int             `gorm:"not null" json:"qty"`
	UnitPrice  decimal.Decimal `gorm:"type:decimal(10,2);not null" json:"unit_price"`
}
