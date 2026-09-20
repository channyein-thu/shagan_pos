package catalog

import (
	"time"

	"github.com/shopspring/decimal"
)

// CreateCategoryRequest is the request body for `POST /categories`. OrgID is
// deliberately not here - a category always belongs to the authenticated
// caller's own organization (see middleware.OrgIDFromContext), never a
// client-supplied org, same reasoning as identity.CreateBranchRequest.
type CreateCategoryRequest struct {
	NameI18n string `json:"name_i18n" binding:"required"`
}

// CreateComboRequest is the request body for the endpoint that creates or updates a Combo.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateComboRequest struct {
	OrgID     uint            `json:"org_id" binding:"required"`
	Name      string          `json:"name" binding:"required"`
	Price     decimal.Decimal `json:"price" binding:"required"`
	ExpiresAt time.Time       `json:"expires_at" binding:"required"`
}

// CreateProductRequest is the request body for `POST /products`. OrgID is
// deliberately not here - same reasoning as CreateCategoryRequest. BranchID
// is a legitimate client choice (an org can have several branches, so the
// caller picks which one, same as identity.CreateStaffRequest) - the service
// verifies it actually belongs to the caller's own org before using it.
// Price, Discount, and Tax carry no binding tag: decimal.Decimal is a
// struct, so go-playground/validator's `required` is a no-op on it and can't
// do numeric comparisons without a custom type registration -
// Service.CreateProduct enforces the real rules (Price > 0, Discount/Tax >=
// 0, Discount <= Price). IsActive also carries no `required` tag
// deliberately - required on a bool rejects its zero value, which would make
// false unrepresentable.
type CreateProductRequest struct {
	BranchID   uint            `json:"branch_id" binding:"required"`
	CategoryID uint            `json:"category_id" binding:"required"`
	Name       string          `json:"name" binding:"required"`
	Barcode    string          `json:"barcode" binding:"required"`
	Price      decimal.Decimal `json:"price"`
	Discount   decimal.Decimal `json:"discount"`
	Tax        decimal.Decimal `json:"tax"`
	Threshold  int             `json:"threshold" binding:"required"`
	IsActive   bool            `json:"is_active"`
	// Modifier is optional and genuinely nullable - nil means no modifier at
	// all (stored as SQL NULL), not an empty string.
	Modifier *string `json:"modifier" binding:"omitempty"`
}

// UpdateCategoryRequest is the request body for `PATCH /categories/:id`.
// OrgID is deliberately not here - a category can never be reassigned to a
// different organization via a client update, same reasoning as
// identity.UpdateBranchRequest.
type UpdateCategoryRequest struct {
	NameI18n *string `json:"name_i18n" binding:"omitempty"`
}

// UpdateComboRequest is the request body for the endpoint that creates or updates a Combo.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateComboRequest struct {
	OrgID     *uint            `json:"org_id" binding:"omitempty"`
	Name      *string          `json:"name" binding:"omitempty"`
	Price     *decimal.Decimal `json:"price" binding:"omitempty"`
	ExpiresAt *time.Time       `json:"expires_at" binding:"omitempty"`
}

// UpdateProductRequest is the request body for the endpoint that creates or updates a Product.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateProductRequest struct {
	OrgID      *uint            `json:"org_id" binding:"omitempty"`
	BranchID   *uint            `json:"branch_id" binding:"omitempty"`
	CategoryID *uint            `json:"category_id" binding:"omitempty"`
	Name       *string          `json:"name" binding:"omitempty"`
	Barcode    *string          `json:"barcode" binding:"omitempty"`
	Price      *decimal.Decimal `json:"price" binding:"omitempty"`
	Discount   *decimal.Decimal `json:"discount" binding:"omitempty"`
	Tax        *decimal.Decimal `json:"tax" binding:"omitempty"`
	Threshold  *int             `json:"threshold" binding:"omitempty"`
	IsActive   *bool            `json:"is_active" binding:"omitempty"`
	Modifier   *string          `json:"modifier" binding:"omitempty"`
}

// ProductImageResult is one image attached to a product, as returned by
// ListProducts/GetProduct. URL is a temporary signed link generated on each
// request (see Service.DefaultImageURLTTL) - StorageKey itself isn't
// directly usable by a client, since the bucket is private.
type ProductImageResult struct {
	ID     uint   `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ProductResult is what ListProducts/GetProduct actually return - the
// Product row plus its images. Product has no Go-level "has many images"
// relation (see the TODO on Product) - this is a response-shaping type
// only, assembled by the service, not a second way of modeling the same
// relationship at the database layer.
type ProductResult struct {
	Product
	Images []ProductImageResult `json:"images"`
}
