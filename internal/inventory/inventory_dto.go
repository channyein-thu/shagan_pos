package inventory

// CreateStockAdjustmentRequest is the request body for the endpoint that creates or updates a StockAdjustment.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateStockAdjustmentRequest struct {
	ProductID uint   `json:"product_id" binding:"required"`
	BranchID  uint   `json:"branch_id" binding:"required"`
	Delta     int    `json:"delta" binding:"required"`
	Reason    string `json:"reason" binding:"required"`
	ActorID   uint   `json:"actor_id" binding:"required"`
}

// CreateStockTransferRequest is the request body for the endpoint that creates or updates a StockTransfer.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateStockTransferRequest struct {
	FromBranch uint           `json:"from_branch" binding:"required"`
	ToBranch   uint           `json:"to_branch" binding:"required"`
	Status     TransferStatus `json:"status" binding:"required"`
	ActorID    uint           `json:"actor_id" binding:"required"`
}

// UpdateStockTransferRequest is the request body for the endpoint that creates or updates a StockTransfer.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateStockTransferRequest struct {
	FromBranch *uint           `json:"from_branch" binding:"omitempty"`
	ToBranch   *uint           `json:"to_branch" binding:"omitempty"`
	Status     *TransferStatus `json:"status" binding:"omitempty"`
	ActorID    *uint           `json:"actor_id" binding:"omitempty"`
}
