package returns

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CreateExchangeRequest is the request body for the endpoint that creates or updates a Exchange.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateExchangeRequest struct {
	SaleID        uuid.UUID       `json:"sale_id" binding:"required"`
	NetDifference decimal.Decimal `json:"net_difference" binding:"required"`
	ApprovedBy    uint            `json:"approved_by" binding:"required"`
}

// CreateReturnRequest is the request body for the endpoint that creates or updates a Return.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateReturnRequest struct {
	SaleID       uuid.UUID        `json:"sale_id" binding:"required"`
	ReasonCode   ReturnReasonCode `json:"reason_code" binding:"required"`
	RefundMethod RefundMethod     `json:"refund_method" binding:"required"`
	RefundTotal  decimal.Decimal  `json:"refund_total" binding:"required"`
	ApprovedBy   uint             `json:"approved_by" binding:"required"`
}

// VoidSaleRequest is the request body for the endpoint that creates or updates a Void.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type VoidSaleRequest struct {
	SaleID      uuid.UUID  `json:"sale_id" binding:"required"`
	SaleItemID  *uint      `json:"sale_item_id"`
	Qty         int        `json:"qty" binding:"required"`
	Reason      VoidReason `json:"reason" binding:"required"`
	Explanation string     `json:"explanation" binding:"required"`
	ApprovedBy  uint       `json:"approved_by" binding:"required"`
}
