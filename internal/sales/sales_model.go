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

// PaymentMethod is confirmed as exactly these 2 values for this version -
// cash and QR (bank QR codes managed via platform.PaymentQRCode). No card,
// mobile wallet, store credit, or other methods are accepted.
type PaymentMethod string

const (
	PaymentMethodCash PaymentMethod = "cash"
	PaymentMethodQR   PaymentMethod = "qr"
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
	// Replayed is never stored or serialized: Service.CreateSale sets it
	// when the call matched an already-stored sale from the same branch and
	// device (an idempotent retry) instead of creating a new one, so the
	// handler can answer 200 rather than 201.
	Replayed bool `gorm:"-" json:"-"`
}

// SameOrigin reports whether this stored sale was rung up at branchID on
// deviceID - the test for "this request is a retry of that sale", as opposed
// to a different till reusing the same client-generated UUID.
func (s *Sale) SameOrigin(branchID, deviceID uint) bool {
	return s.BranchID == branchID && s.DeviceID == deviceID
}

// SaleItem maps to the "Sale_items" table in the ERD. ComboID is set when
// this line is one component of a Combo rung up as a group - the combo is
// still expanded into one SaleItem per real product (so stock/inventory
// effects work exactly like any other line item, see the CreateSaleItemRequest
// doc comment on Combo pricing), ComboID just tags them as belonging
// together for receipts/reporting.
type SaleItem struct {
	ID            uint             `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID        uuid.UUID        `gorm:"type:uuid;index;not null" json:"sale_id"`
	ProductID     uint             `gorm:"index;not null" json:"product_id"`
	ComboID       *uint            `gorm:"index" json:"combo_id"`
	NameSnapshot  string           `gorm:"size:255;not null" json:"name_snapshot"`
	UnitPrice     decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"unit_price"`
	PriceOverride *decimal.Decimal `gorm:"type:decimal(10,2)" json:"price_override"`
	Qty           int              `gorm:"not null" json:"qty"`
	LineTotal     decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"line_total"`
	Discount      decimal.Decimal  `gorm:"type:decimal(10,2);not null" json:"discount"`
	// Tax is this line's own tax amount (qty already accounted for), snapshotted
	// by the client from Product.Tax at ring-up time - same offline-first
	// reasoning as UnitPrice/NameSnapshot. Summed into Sale.Tax rather than
	// the caller supplying one flat sale-level tax figure.
	Tax decimal.Decimal `gorm:"type:decimal(10,2);not null;default:0" json:"tax"`
	// UnitCost is a snapshot of Product.CostPrice taken server-side at sale
	// time (never client-supplied, unlike UnitPrice/NameSnapshot/Tax - cost
	// is business-sensitive and a till has no business knowing it - see
	// catalog.Product.CostPrice's own doc). Snapshotting it here, not just
	// reading Product.CostPrice at report time, keeps a past sale's
	// reported profit from silently changing if the product's cost changes
	// later.
	UnitCost decimal.Decimal `gorm:"type:decimal(10,2);not null;default:0" json:"-"`
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
