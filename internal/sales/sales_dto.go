package sales

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// CreateHeldSaleRequest is the request body for `POST /held-sales` - a
// cashier parking their current cart to serve someone else, resumed later
// via ResumeHeldSale. BranchID/StaffID aren't binding:"required" - same
// reasoning as CreateSaleRequest.StaffID, both are always overwritten from
// the caller's own access token/X-Staff-Token, never client input. HeldAt
// likewise isn't required - it's server-set to now(), same reasoning as
// identity.OpenShiftRequest's lifecycle fields. Items is an opaque
// client-owned cart snapshot (JSONB) - the server never inspects its
// structure, it's just stored and handed back verbatim on Resume.
type CreateHeldSaleRequest struct {
	BranchID    uint            `json:"branch_id"`
	StaffID     uint            `json:"staff_id"`
	CustomerRef *uint           `json:"customer_ref"`
	Items       datatypes.JSON  `json:"items" binding:"required"`
	Discount    decimal.Decimal `json:"discount"`
	HeldAt      time.Time       `json:"held_at"`
}

// CreateSaleItemRequest is one line item of a CreateSaleRequest. NameSnapshot,
// UnitPrice, and Tax are all captured by the POS device from its own local
// product cache at ring-up time (UnitPrice from Product.Price, Tax from
// Product.Tax), not re-fetched from Catalog here - this domain is
// offline-first, and the device may have rung the item up while offline.
// Tax is this line's own tax amount (already accounts for Qty, not a
// per-unit rate) - Sale.Tax is the sum across all items, there's no
// separate sale-level tax input.
//
// ComboID is set when this item is one component of a Combo rung up as a
// group - the POS device expands the combo into one CreateSaleItemRequest
// per real product (each carrying its own UnitPrice/Tax/a proportional share
// of the combo's bundle discount), rather than the server doing any
// combo-aware pricing itself. That keeps every line item structurally
// identical to a standalone product sale - stock decrement (once Inventory
// exists) and reporting both work per real product with no special-casing,
// and ComboID just tags the lines as belonging to the same combo for
// receipts/reporting.
type CreateSaleItemRequest struct {
	ProductID     uint             `json:"product_id" binding:"required"`
	ComboID       *uint            `json:"combo_id"`
	NameSnapshot  string           `json:"name_snapshot" binding:"required"`
	UnitPrice     decimal.Decimal  `json:"unit_price" binding:"required"`
	PriceOverride *decimal.Decimal `json:"price_override"`
	Qty           int              `json:"qty" binding:"required,gt=0"`
	Discount      decimal.Decimal  `json:"discount"`
	Tax           decimal.Decimal  `json:"tax"`
}

// CreateSalePaymentRequest is one payment applied to a CreateSaleRequest -
// there can be more than one (split payment, e.g. part cash part QR).
type CreateSalePaymentRequest struct {
	Method         PaymentMethod   `json:"method" binding:"required,oneof=cash card qr mobile_wallet store_credit other"`
	Amount         decimal.Decimal `json:"amount" binding:"required"`
	AmountReceived decimal.Decimal `json:"amount_received"`
	ChangeGiven    decimal.Decimal `json:"change_given"`
}

// CreateSaleRequest is the request body for `POST /sales`. OrgID isn't here -
// it comes from the authenticated caller's own organization. BranchID isn't
// here either - a sale is rung up by a branch-bound pos device, so BranchID
// comes from its access token (middleware.BranchIDFromContext), never
// client input, same reasoning as identity's branch-scoped reads.
//
// Subtotal/Discount/Tax/Total are deliberately NOT accepted here - the
// service derives Subtotal/Discount/Tax from Items (each item snapshots its
// own Tax - see CreateSaleItemRequest) and validates Payments sum to the
// derived Total, rather than trusting client-computed totals. Status and
// CompletedAt are also server-owned - creating a sale always completes it
// immediately, same reasoning as identity.OpenShiftRequest's lifecycle
// fields. StaffID isn't binding:"required" for the same reason it isn't on
// shift.OpenShiftRequest - it's always overwritten from the verified
// X-Staff-Token, never a client-supplied staff id (see cmd/api/sales.go's
// CreateSale handler). That verified identity is also what
// Service.CreateSale checks apply_manual_discount against.
type CreateSaleRequest struct {
	ID         uuid.UUID                  `json:"id" binding:"required"`
	ShiftID    uint                       `json:"shift_id" binding:"required"`
	StaffID    uint                       `json:"staff_id"`
	DeviceID   uint                       `json:"device_id" binding:"required"`
	CustomerID *uint                      `json:"customer_id"`
	Items      []CreateSaleItemRequest    `json:"items" binding:"required,min=1,dive"`
	Payments   []CreateSalePaymentRequest `json:"payments" binding:"required,min=1,dive"`
}
