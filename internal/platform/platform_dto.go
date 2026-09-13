package platform

// UpdateReceiptSettingsRequest is the request body for the endpoint that creates or updates a ReceiptSetting.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateReceiptSettingsRequest struct {
	OrgID    *uint   `json:"org_id" binding:"omitempty"`
	BranchID *uint   `json:"branch_id" binding:"omitempty"`
	ShopName *string `json:"shop_name" binding:"omitempty"`
	Address  *string `json:"address" binding:"omitempty"`
	Phone    *string `json:"phone" binding:"omitempty"`
	ThankYou *string `json:"thank_you" binding:"omitempty"`
	IsGlobal *bool   `json:"is_global" binding:"omitempty"`
}
