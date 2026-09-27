package inventory

// CreateStockAdjustmentRequest is the request body for `POST
// /inventory/adjustments`. BranchID isn't here - the adjustment always
// applies to the branch in.ProductID's own Product row belongs to (a
// product lives at exactly one branch), never a client-supplied one.
// ActorID isn't here either - it's the authenticated caller's own user ID,
// never client input, same reasoning as procurement.CreateGoodsReceiptRequest.
type CreateStockAdjustmentRequest struct {
	ProductID uint   `json:"product_id" binding:"required"`
	Delta     int    `json:"delta" binding:"required"`
	Reason    string `json:"reason" binding:"required"`
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
