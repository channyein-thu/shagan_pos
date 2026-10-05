package platform

// UpdateReceiptSettingsRequest is the request body for `PUT /receipt-settings`.
// BranchID targets a branch-specific override when set; omitted, it updates
// the org-wide default (BranchID IS NULL, IsGlobal true). OrgID always comes
// from the authenticated caller's token, never the request body.
type UpdateReceiptSettingsRequest struct {
	BranchID *uint  `json:"branch_id" binding:"omitempty"`
	ShopName string `json:"shop_name" binding:"required"`
	Address  string `json:"address" binding:"required"`
	Phone    string `json:"phone" binding:"required"`
	ThankYou string `json:"thank_you" binding:"required"`
}

// PaymentQRCodeResult is a PaymentQRCode with a temporary signed image URL in
// place of the raw StorageKey - same reasoning as catalog.ProductImageResult,
// the bucket is private so a raw key isn't usable by a client.
type PaymentQRCodeResult struct {
	PaymentQRCode
	ImageURL string `json:"image_url"`
}
