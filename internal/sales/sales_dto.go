package sales

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

// CreateHeldSaleRequest is the request body for the endpoint that creates or updates a HeldSale.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateHeldSaleRequest struct {
	BranchID    uint            `json:"branch_id" binding:"required"`
	StaffID     uint            `json:"staff_id" binding:"required"`
	CustomerRef *uint           `json:"customer_ref"`
	Items       datatypes.JSON  `json:"items" binding:"required"`
	Discount    decimal.Decimal `json:"discount" binding:"required"`
	HeldAt      time.Time       `json:"held_at" binding:"required"`
}

// CreateSaleItemRequest is one line item of a CreateSaleRequest. NameSnapshot
// and UnitPrice are captured by the POS device from its own local product
// cache at ring-up time, not re-fetched from Catalog here - this domain is
// offline-first, and the device may have rung the item up while offline.
type CreateSaleItemRequest struct {
	ProductID     uint             `json:"product_id" binding:"required"`
	NameSnapshot  string           `json:"name_snapshot" binding:"required"`
	UnitPrice     decimal.Decimal  `json:"unit_price" binding:"required"`
	PriceOverride *decimal.Decimal `json:"price_override"`
	Qty           int              `json:"qty" binding:"required,gt=0"`
	Discount      decimal.Decimal  `json:"discount"`
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
// service derives them from Items (and validates Payments sum to the
// derived Total) rather than trusting client-computed totals. Status and
// CompletedAt are also server-owned - creating a sale always completes it
// immediately, same reasoning as identity.OpenShiftRequest's lifecycle
// fields.
type CreateSaleRequest struct {
	ID         uuid.UUID                  `json:"id" binding:"required"`
	ShiftID    uint                       `json:"shift_id" binding:"required"`
	StaffID    uint                       `json:"staff_id" binding:"required"`
	DeviceID   uint                       `json:"device_id" binding:"required"`
	CustomerID *uint                      `json:"customer_id"`
	Tax        decimal.Decimal            `json:"tax"`
	Items      []CreateSaleItemRequest    `json:"items" binding:"required,min=1,dive"`
	Payments   []CreateSalePaymentRequest `json:"payments" binding:"required,min=1,dive"`
}
