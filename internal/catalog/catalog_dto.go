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

// CreateProductRequest is the request body for the endpoint that creates or updates a Product.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateProductRequest struct {
	OrgID       uint            `json:"org_id" binding:"required"`
	BranchScope BranchScope     `json:"branch_scope" binding:"required"`
	CategoryID  uint            `json:"category_id" binding:"required"`
	Name        string          `json:"name" binding:"required"`
	Barcode     string          `json:"barcode" binding:"required"`
	Price       decimal.Decimal `json:"price" binding:"required"`
	CostPrice   decimal.Decimal `json:"cost_price" binding:"required"`
	Discount    decimal.Decimal `json:"discount" binding:"required"`
	Tax         decimal.Decimal `json:"tax" binding:"required"`
	Threshold   int             `json:"threshold" binding:"required"`
	IsActive    bool            `json:"is_active" binding:"required"`
	Modifier    string          `json:"modifier" binding:"required"`
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
	OrgID       *uint            `json:"org_id" binding:"omitempty"`
	BranchScope *BranchScope     `json:"branch_scope" binding:"omitempty"`
	CategoryID  *uint            `json:"category_id" binding:"omitempty"`
	Name        *string          `json:"name" binding:"omitempty"`
	Barcode     *string          `json:"barcode" binding:"omitempty"`
	Price       *decimal.Decimal `json:"price" binding:"omitempty"`
	CostPrice   *decimal.Decimal `json:"cost_price" binding:"omitempty"`
	Discount    *decimal.Decimal `json:"discount" binding:"omitempty"`
	Tax         *decimal.Decimal `json:"tax" binding:"omitempty"`
	Threshold   *int             `json:"threshold" binding:"omitempty"`
	IsActive    *bool            `json:"is_active" binding:"omitempty"`
	Modifier    *string          `json:"modifier" binding:"omitempty"`
}
