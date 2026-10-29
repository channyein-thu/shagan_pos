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

// CreateSaleItemRequest is one line of a CreateSaleRequest. The server prices
// every line from the PRODUCT, not from this request:
//
//   - the unit price is the product's current price. UnitPrice is only checked
//     (a different non-zero value is a 409 "price changed, refresh the catalog");
//     leaving it out is fine.
//   - with no DiscountApprovalID the line gets the product's catalog % (its
//     sale_price); with one, the approval's % comes off the ORIGINAL price and
//     replaces the catalog %.
//   - Discount is IGNORED on every line, combo lines included: the client never
//     gets to say how much was taken off. A combo line's share of the bundle
//     discount is computed by the server from the combo's own price (stored as
//     combo_discount; it needs no approval and is not a manual discount). A
//     till that still sends one is not read - the field only stays so older
//     tills don't break.
//   - PriceOverride is rejected (400).
//   - there is no tax field: the product's tax % is inside its price, and the
//     server works out each line's tax from the line total. A `tax` an older
//     till still sends is not read.
//
// The same product may appear on several lines - the till can split 1 of 2
// coffees onto its own discounted line.
//
// ComboID is set when the line is one component of a Combo rung up as a group: the till expands the combo into
// one line per real product, so every line stays structurally identical to a
// standalone product sale. The server then checks the combo is real (404
// otherwise), not expired (409), and that the lines under one combo_id are
// exactly its components with the same whole number of bundles (400
// otherwise), and it works out each line's share of the bundle discount so
// the lines add up to exactly combo price x bundles. See Service.priceCombos.
//
// CatalogDiscountPercent and TaxPercent are read ONLY for an offline-queued
// sale (POST /sync/sales): that sale was priced from the till's cached catalog,
// so its UnitPrice, catalog % and tax % snapshot are taken as rung up (and a
// conflict raised if any has gone stale). On a live sale they are ignored.
type CreateSaleItemRequest struct {
	ProductID              uint             `json:"product_id" binding:"required"`
	ComboID                *uint            `json:"combo_id"`
	NameSnapshot           string           `json:"name_snapshot" binding:"required"`
	UnitPrice              decimal.Decimal  `json:"unit_price"`
	PriceOverride          *decimal.Decimal `json:"price_override"`
	Qty                    int              `json:"qty" binding:"required,gt=0"`
	Discount               decimal.Decimal  `json:"discount"`
	DiscountApprovalID     *uuid.UUID       `json:"discount_approval_id"`
	CatalogDiscountPercent *decimal.Decimal `json:"catalog_discount_percent"`
	// TaxPercent is read ONLY for an offline-queued sale: the tax rate in the
	// till's cached catalog when the line was rung up (0-100). Omitted = the
	// product's current rate.
	TaxPercent *decimal.Decimal `json:"tax_percent"`
}

// CreateSalePaymentRequest is one payment applied to a CreateSaleRequest -
// there can be more than one (split payment, e.g. part cash part QR).
type CreateSalePaymentRequest struct {
	Method         PaymentMethod   `json:"method" binding:"required,oneof=cash qr"`
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
// service derives them from Items (each priced from its product - see
// CreateSaleItemRequest) and validates Payments sum to the derived Total
// (Subtotal - Discount: tax is inside the prices, never added), rather than
// trusting client-computed totals. Status and
// CompletedAt are also server-owned - creating a sale always completes it
// immediately, same reasoning as identity.OpenShiftRequest's lifecycle
// fields. StaffID isn't binding:"required" for the same reason it isn't on
// shift.OpenShiftRequest - it's always overwritten from the verified
// X-Staff-Token, never a client-supplied staff id (see cmd/api/sales.go's
// CreateSale handler). That verified identity is also the seller a custom
// discount approval must have been requested by.
type CreateSaleRequest struct {
	ID      uuid.UUID `json:"id" binding:"required"`
	ShiftID uint      `json:"shift_id" binding:"required"`
	// RungUpAt is read ONLY for an offline-queued sale: when the till rang it
	// up, so a combo is judged expired as of the sale, not as of the (later)
	// sync. Omitted or in the future = the time the server received it. On a
	// live sale it is ignored.
	RungUpAt   *time.Time                 `json:"rung_up_at"`
	StaffID    uint                       `json:"staff_id"`
	DeviceID   uint                       `json:"device_id" binding:"required"`
	CustomerID *uint                      `json:"customer_id"`
	Items      []CreateSaleItemRequest    `json:"items" binding:"required,min=1,dive"`
	Payments   []CreateSalePaymentRequest `json:"payments" binding:"required,min=1,dive"`
}

// SaleFilter narrows and pages `GET /sales`. Start/End are the already-resolved
// half-open [Start, End) bounds on Sale.CompletedAt (nil = unbounded on that
// side) and Page/PageSize are already normalized - see Service.ListSales.
type SaleFilter struct {
	BranchID *uint
	// StaffID restricts to the sales one staff member rang up - set for a
	// cashier whose role lacks view_all_receipts.
	StaffID *uint
	// ShiftID restricts to the sales rung up during one shift (Back Office's
	// link from a shift to its sales).
	ShiftID  *uint
	Start    *time.Time
	End      *time.Time
	Page     int
	PageSize int
}

// ReturnSummary is a sale's return/exchange footprint, shown on history rows
// and the receipt so the till doesn't have to load every return and exchange
// to find out. RefundedTotal is the sum of the sale's Returns' refunds
// (exchanges settle a difference, not a refund, so they are not in it).
type ReturnSummary struct {
	HasReturn     bool            `json:"has_return"`
	HasExchange   bool            `json:"has_exchange"`
	RefundedTotal decimal.Decimal `json:"refunded_total"`
}

// ReceiptSale is `sale` in the receipt bundle: the Sale plus its
// ReturnSummary, flattened into one JSON object.
type ReceiptSale struct {
	Sale
	ReturnSummary
}

// ReceiptItem is one line of the receipt bundle: the SaleItem plus how much
// of it has already come back (returned on a Return, or exchanged in) and
// what the till may still offer. ReturnableQty = Qty - ReturnedQty, never
// below zero.
type ReceiptItem struct {
	SaleItem
	ReturnedQty   int `json:"returned_qty"`
	ReturnableQty int `json:"returnable_qty"`
	// UnitSalePrice is the unit price after the catalog or manual discount
	// (before any combo share): UnitPrice - (catalog + manual) / Qty.
	UnitSalePrice decimal.Decimal `json:"unit_sale_price"`
	// ManualApprovedByName is the name of ManualApprovedBy, "" with no manual
	// discount.
	ManualApprovedByName string `json:"manual_approved_by_name"`
}

// SaleListItem is one row of `GET /sales`: the Sale plus how it was paid, so
// a history screen can show the method without a receipt fetch per row.
// PaymentMethods is every distinct method used on the sale, sorted
// ("cash", "qr", or both for a split payment) - never null.
type SaleListItem struct {
	Sale
	ReturnSummary
	PaymentMethods []PaymentMethod `json:"payment_methods"`
}

// SalesPage is the `GET /sales` response - same envelope as
// reports.TransactionsReport.
type SalesPage struct {
	Sales      []SaleListItem `json:"sales"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalCount int64          `json:"total_count"`
}

// PriceMismatch is one line of an offline-queued sale whose till-cached price,
// catalog % or tax % no longer matches the product. Raised as a sync conflict;
// the sale is still accepted as it was rung up.
//
// A combo that has changed price since the till cached it is reported once per
// combo, not per line: ProductID is 0 and ComboID, ClientBundleTotal (what the
// till charged for the bundle(s)) and CurrentBundleTotal (what the server
// computed from the combo's current price) say what differed.
type PriceMismatch struct {
	ProductID         uint            `json:"product_id"`
	ClientUnitPrice   decimal.Decimal `json:"client_unit_price"`
	ClientPercent     decimal.Decimal `json:"client_percent"`
	ClientTaxPercent  decimal.Decimal `json:"client_tax_percent"`
	CurrentUnitPrice  decimal.Decimal `json:"current_unit_price"`
	CurrentPercent    decimal.Decimal `json:"current_percent"`
	CurrentTaxPercent decimal.Decimal `json:"current_tax_percent"`

	ComboID            *uint            `json:"combo_id,omitempty"`
	ClientBundleTotal  *decimal.Decimal `json:"client_bundle_total,omitempty"`
	CurrentBundleTotal *decimal.Decimal `json:"current_bundle_total,omitempty"`
}

// CreateDiscountApprovalRequest is the request body for
// `POST /sales/discount-approvals`: the seller asks to give ProductID a custom
// Percent off for up to Qty units. ApproverStaffID + Pin identify who approves
// it; leave both out for self-approval (the seller holds both
// approve_manual_discount and approve_own_discount). The seller is always the
// X-Staff-Token's staff member, never a body field.
type CreateDiscountApprovalRequest struct {
	ProductID       uint            `json:"product_id" binding:"required"`
	Percent         decimal.Decimal `json:"percent"`
	Qty             int             `json:"qty" binding:"required,gt=0"`
	Reason          string          `json:"reason" binding:"max=200"`
	ApproverStaffID uint            `json:"approver_staff_id"`
	Pin             string          `json:"pin"`
}

// ApproverRef names who approved a discount.
type ApproverRef struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// DiscountApprovalResult is what the till gets back: the approval plus the
// prices the backend WILL charge, so the till shows exactly them.
type DiscountApprovalResult struct {
	ID            uuid.UUID       `json:"id"`
	ProductID     uint            `json:"product_id"`
	Percent       decimal.Decimal `json:"percent"`
	MaxQty        int             `json:"max_qty"`
	UnitPrice     decimal.Decimal `json:"unit_price"`
	UnitSalePrice decimal.Decimal `json:"unit_sale_price"`
	ApprovedBy    ApproverRef     `json:"approved_by"`
	ExpiresAt     time.Time       `json:"expires_at"`
}

// DiscountSeller is the authenticated staff member asking for a discount,
// resolved by the handler from the till's tokens (never client input).
type DiscountSeller struct {
	StaffID     uint
	PosUserID   uint // the paired POS account of the till - what finds its open shift
	Permissions []string
}

// OpenShift is the till's currently open shift.
type OpenShift struct {
	ID       uint
	BranchID uint
}
