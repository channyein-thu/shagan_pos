package inventory

import "github.com/shopspring/decimal"

// CreateStockAdjustmentRequest is the request body for `POST
// /inventory/adjustments`. BranchID is a required client choice - products
// are org-wide (see catalog.Product's doc), so the branch whose StockLevel
// is being adjusted can no longer be derived from the product itself; the
// service verifies it actually belongs to the caller's own org, same
// reasoning as procurement.CreatePurchaseOrderRequest. ActorID isn't here -
// it's the authenticated caller's own user ID, never client input, same
// reasoning as procurement.CreateGoodsReceiptRequest. UnitCost is optional
// and only meaningful when Delta is positive (you're adding stock, so you
// can say what it's worth) - when given, it blends into the product's
// CostPrice by the same weighted-average method a Procurement goods receipt
// uses (see catalog.Product.CostPrice's own doc), closing the gap for stock
// that enters purely through a manual count/correction rather than a real
// purchase order. Rejected (400) if given alongside a zero-or-negative Delta.
type CreateStockAdjustmentRequest struct {
	BranchID  uint             `json:"branch_id" binding:"required"`
	ProductID uint             `json:"product_id" binding:"required"`
	Delta     int              `json:"delta" binding:"required"`
	Reason    string           `json:"reason" binding:"required"`
	UnitCost  *decimal.Decimal `json:"unit_cost"`
}

// CreateStockTransferItemRequest is one product line within
// CreateStockTransferRequest.
type CreateStockTransferItemRequest struct {
	ProductID uint `json:"product_id" binding:"required"`
	Qty       int  `json:"qty" binding:"required,gt=0"`
}

// CreateStockTransferRequest is the request body for `POST
// /stock-transfers`. Status isn't here - a new transfer always starts at
// TransferStatusPending; moving it along the lifecycle happens via
// UpdateStockTransfer. ActorID isn't here either - it's the authenticated
// caller's own user ID, never client input. Items must contain at least one
// entry - a transfer moving nothing isn't meaningful.
type CreateStockTransferRequest struct {
	FromBranch uint                             `json:"from_branch" binding:"required"`
	ToBranch   uint                             `json:"to_branch" binding:"required"`
	Items      []CreateStockTransferItemRequest `json:"items" binding:"required,min=1,dive"`
}

// UpdateStockTransferRequest is the request body for `PATCH
// /stock-transfers/:id`. Only Status can change - FromBranch/ToBranch/Items
// are fixed at creation (moving stock from a branch that was never the
// sender, or between branches that don't match what was actually created,
// isn't a "transfer update", it's a different transfer). See
// Service.UpdateStockTransfer for what moving to TransferStatusCompleted
// actually does.
type UpdateStockTransferRequest struct {
	Status TransferStatus `json:"status" binding:"required,oneof=in_transit completed cancelled"`
}
