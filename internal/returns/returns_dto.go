package returns

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// VoidSaleRequest is the request body for `POST /sales/:id/void`. SaleID
// comes from the URL path, not this body. A void always reverses the
// entire sale - there is no partial void (see docs/WORKFLOWS.md Section 9) -
// so unlike a Return, there's no items list here. ApprovedBy isn't here -
// it's the authenticated caller's own user ID, granted either because they
// hold approve_void themselves or via a manager's X-Manager-Approval-Token,
// same reasoning as sales.CreateSaleRequest's actor handling.
type VoidSaleRequest struct {
	Reason      VoidReason `json:"reason" binding:"required"`
	Explanation string     `json:"explanation" binding:"required"`
}

// CreateReturnItemRequest is one line within CreateReturnRequest - the
// original SaleItem being (partially or fully) returned, and the condition
// it came back in. Only Condition ItemConditionSellable is restocked (see
// Service.CreateReturn's doc) - a conservative default given the model
// doesn't specify which of the other conditions (damaged/opened/defective)
// a store would still resell.
type CreateReturnItemRequest struct {
	SaleItemID uint          `json:"sale_item_id" binding:"required"`
	Qty        int           `json:"qty" binding:"required,gt=0"`
	Condition  ItemCondition `json:"condition" binding:"required"`
}

// CreateReturnRequest is the request body for `POST /returns`. RefundTotal
// isn't here - it's computed server-side from each returned item's own
// original per-unit price (never trust a client-computed total, same
// reasoning as sales.CreateSaleRequest deriving Subtotal/Total itself).
// ApprovedBy isn't here either - same reasoning as VoidSaleRequest, checked
// against approve_return. RefundMethod is always an explicit single choice
// by the approver (cash or qr) - never derived from how the original sale
// was paid (confirmed: refunds don't try to mirror a split original
// payment).
type CreateReturnRequest struct {
	SaleID       uuid.UUID                 `json:"sale_id" binding:"required"`
	Items        []CreateReturnItemRequest `json:"items" binding:"required,min=1,dive"`
	ReasonCode   ReturnReasonCode          `json:"reason_code" binding:"required"`
	RefundMethod RefundMethod              `json:"refund_method" binding:"required,oneof=cash qr"`
}

// CreateExchangeItemRequest is one line within CreateExchangeRequest.
// Direction "in" is an item the customer is returning - SaleItemID must
// reference an original line on the sale, and its price is derived from
// that SaleItem, never client input (UnitPrice is ignored for an "in"
// line). Direction "out" is a new item the customer is taking instead -
// ProductID and UnitPrice are required, same never-trust-client-price
// reasoning as sales.CreateSaleItemRequest doesn't quite apply here since
// there's no product catalog price snapshot step in this domain; the
// approver is expected to enter the product's current price.
type CreateExchangeItemRequest struct {
	Direction  Direction        `json:"direction" binding:"required,oneof=in out"`
	SaleItemID *uint            `json:"sale_item_id"`
	ProductID  *uint            `json:"product_id"`
	Qty        int              `json:"qty" binding:"required,gt=0"`
	UnitPrice  *decimal.Decimal `json:"unit_price"`
}

// CreateExchangeRequest is the request body for `POST /exchanges`.
// NetDifference isn't here - computed server-side from Items (same
// never-trust-client-totals reasoning as CreateReturnRequest). ApprovedBy
// isn't here either - same reasoning as VoidSaleRequest, checked against
// approve_exchange. Modeled as one combined transaction (not a chained
// Return-then-Sale) - see Service.CreateExchange's doc.
type CreateExchangeRequest struct {
	SaleID uuid.UUID                   `json:"sale_id" binding:"required"`
	Items  []CreateExchangeItemRequest `json:"items" binding:"required,min=1,dive"`
}
