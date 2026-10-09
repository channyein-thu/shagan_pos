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

	// The till's own void reasons (shagan-retail void-select-step.tsx). The
	// five above stay because the owner back office still uses them.
	VoidReasonDuplicateTransaction VoidReason = "duplicate_transaction"
	VoidReasonWrongOrder           VoidReason = "wrong_order"
	VoidReasonIncorrectPayment     VoidReason = "incorrect_payment"
	VoidReasonCashierMistake       VoidReason = "cashier_mistake"
)

// ReturnReasonCode is a best-guess enum (ERD only specified "enum"; confirm real values).
type ReturnReasonCode string

const (
	ReturnReasonCodeDefective   ReturnReasonCode = "defective"
	ReturnReasonCodeWrongItem   ReturnReasonCode = "wrong_item"
	ReturnReasonCodeChangedMind ReturnReasonCode = "changed_mind"
	ReturnReasonCodeOther       ReturnReasonCode = "other"
)

// RefundMethod - confirmed: the only real payment methods in this POS are
// cash and QR (plus split, but a refund is always a single explicit choice
// by the approving manager, never derived from the original sale's payment
// mix - see CreateReturnRequest's doc).
type RefundMethod string

const (
	RefundMethodCash RefundMethod = "cash"
	RefundMethodQR   RefundMethod = "qr"
)

// ItemCondition is a best-guess enum (ERD only specified "enum"; confirm real values).
type ItemCondition string

const (
	ItemConditionSellable  ItemCondition = "sellable"
	ItemConditionDamaged   ItemCondition = "damaged"
	ItemConditionOpened    ItemCondition = "opened"
	ItemConditionDefective ItemCondition = "defective"
	// The till's own Expired / Other conditions (shagan-retail
	// return-reason-step.tsx). Like every condition but sellable, they are
	// not restocked.
	ItemConditionExpired ItemCondition = "expired"
	ItemConditionOther   ItemCondition = "other"
)

// ExchangeMethod is how an Exchange's non-zero net_difference was settled
// at the till - cash and QR only, same as RefundMethod. It's what lets
// Close Shift tell a cash difference (touches the drawer) from a QR one
// (doesn't).
type ExchangeMethod string

const (
	ExchangeMethodCash ExchangeMethod = "cash"
	ExchangeMethodQR   ExchangeMethod = "qr"
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
	// ApprovedBy is the staff member (own permission, or via a manager's
	// approval token) who approved the void; nil when the Owner voided the
	// sale directly (the Owner has no Staff record - see ApprovedByUserID).
	// Exactly one of ApprovedBy/ApprovedByUserID is set.
	ApprovedBy *uint `gorm:"index" json:"approved_by"`
	// ApprovedByUserID is the org-wide account (identity.User, an Owner or
	// Service Center) that voided the sale directly, with no PIN.
	ApprovedByUserID *uint     `gorm:"index" json:"approved_by_user_id"`
	CreatedAt        time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
}

// Return maps to the "Returns" table in the ERD.
type Return struct {
	ID           uint             `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID       uuid.UUID        `gorm:"type:uuid;index;not null" json:"sale_id"`
	ReasonCode   ReturnReasonCode `gorm:"type:varchar(30);not null" json:"reason_code"`   // one of ReturnReasonCode* constants below (TODO: confirm real values)
	RefundMethod RefundMethod     `gorm:"type:varchar(30);not null" json:"refund_method"` // one of RefundMethod* constants below (TODO: confirm real values)
	RefundTotal  decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"refund_total"`
	// Explanation is the free-text "reason for return" the cashier types at
	// the till, kept alongside the coarse ReasonCode. "" for older rows.
	Explanation string `gorm:"type:text;not null;default:''" json:"explanation"`
	ApprovedBy  uint   `gorm:"index;not null" json:"approved_by"`
	// ShiftID is the till's own open shift when the return was processed -
	// NOT the original sale's shift (the sale may be from days ago; the
	// refund leaves whatever drawer is open now). Nil for rows from before
	// this column existed, or when the caller had no open shift. Close
	// Shift's expected cash subtracts cash refunds by this.
	ShiftID   *uint     `gorm:"index" json:"shift_id"`
	CreatedAt time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
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
	// Method is how NetDifference was settled (cash/QR); nil when it was
	// zero, and for rows from before this column existed.
	Method *ExchangeMethod `gorm:"type:varchar(30)" json:"method"`
	// ShiftID is the till's own open shift when the exchange was processed -
	// same meaning as Return.ShiftID.
	ShiftID   *uint     `gorm:"index" json:"shift_id"`
	CreatedAt time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
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
	// Condition is the state an "in" line came back in; only sellable goes
	// back on the shelf. Nil for "out" lines and for rows from before this
	// column existed (those were all restocked, i.e. sellable).
	Condition *ItemCondition `gorm:"type:varchar(30)" json:"condition"`
}
