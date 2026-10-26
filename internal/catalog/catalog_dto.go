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

// CreateComboRequest is the request body for `POST /combos`. Bound from a
// multipart form, not JSON - `image` is an optional file field alongside
// these (see cmd/api.CatalogAPI.CreateCombo), so the struct binding tags
// below are never actually evaluated (same situation as
// CreateProductRequest already is); they document the shape only. OrgID is
// deliberately not here - same reasoning as CreateCategoryRequest. Price
// carries no binding tag - same reasoning as CreateProductRequest's Price:
// decimal.Decimal's zero value only catches an omitted field, not an
// explicit "price": 0, so Service.CreateCombo validates it's actually
// positive. Items must contain at least one entry with no duplicate
// ProductID - a combo bundles at least one product, and listing the same
// product twice makes no sense; the handler/service enforce this manually
// (see the same not-a-JSON-bind reasoning above). Each ProductID's
// ownership is checked against orgID by the service, same reasoning as
// CreateProduct's branch/category ownership checks.
type CreateComboRequest struct {
	Name      string                   `json:"name" binding:"required"`
	Price     decimal.Decimal          `json:"price"`
	ExpiresAt time.Time                `json:"expires_at" binding:"required"`
	Items     []CreateComboItemRequest `json:"items" binding:"required,min=1,unique=ProductID,dive"`
}

// CreateComboItemRequest is one bundled product within CreateComboRequest.
type CreateComboItemRequest struct {
	ProductID uint `json:"product_id" binding:"required"`
	Qty       int  `json:"qty" binding:"min=1"`
}

// CreateProductRequest is the request body for `POST /products`. OrgID is
// deliberately not here - same reasoning as CreateCategoryRequest. Products
// are org-wide, not branch-scoped, so there's no BranchID either - stock at
// a given branch is established separately via Inventory/Procurement.
// Price, DiscountPercent and TaxPercent carry no binding tag: decimal.Decimal is a
// struct, so go-playground/validator's `required` is a no-op on it and can't
// do numeric comparisons without a custom type registration -
// Service.CreateProduct enforces the real rules (Price > 0,
// 0 <= DiscountPercent <= 100, 0 <= TaxPercent <= 100). There is deliberately
// no discount or tax *amount* field: the server computes sale_price, the
// per-unit discount and the tax included in the price from the percents, and a
// client-sent `discount` or `tax` is ignored. IsActive also carries no
// `required` tag
// deliberately - required on a bool rejects its zero value, which would make
// false unrepresentable.
type CreateProductRequest struct {
	CategoryID uint            `json:"category_id" binding:"required"`
	Name       string          `json:"name" binding:"required"`
	Barcode    string          `json:"barcode" binding:"required"`
	Price      decimal.Decimal `json:"price"`
	// DiscountPercent is the catalog discount, 0-100 (omitted = 0).
	DiscountPercent decimal.Decimal `json:"discount_percent"`
	// TaxPercent is the tax already inside the price, 0-100 (omitted = 0).
	TaxPercent decimal.Decimal `json:"tax_percent"`
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

// UpdateComboRequest is the request body for `PATCH /combos/:id`. Bound
// from a multipart form, not JSON - `image` is an optional file field
// alongside these (see cmd/api.CatalogAPI.UpdateCombo), so the struct
// binding tags below are never actually evaluated (same situation as
// UpdateProductRequest already is); they document the shape only. OrgID is
// deliberately not here - a combo can never be reassigned to a different
// organization via a client update, same reasoning as
// identity.UpdateBranchRequest. Doesn't support editing Items yet - only
// the combo's own fields and its image.
type UpdateComboRequest struct {
	Name      *string          `json:"name" binding:"omitempty"`
	Price     *decimal.Decimal `json:"price" binding:"omitempty"`
	ExpiresAt *time.Time       `json:"expires_at" binding:"omitempty"`
}

// UpdateProductRequest is the request body for `PATCH /products/:id`. Bound
// from a multipart form, not JSON - `image` is an optional file field
// alongside these (see cmd/api.CatalogAPI.UpdateProduct), so the struct
// binding tags below are never actually evaluated (same situation as
// CreateProductRequest already is); they document the shape only. The
// handler uses gin's GetPostForm (not PostForm) to tell "field omitted"
// from "field sent" for each one, since a plain multipart form field can't
// otherwise distinguish the two. OrgID is deliberately not here - a product
// can never be reassigned to a different organization via a client update,
// same reasoning as identity.UpdateBranchRequest. There's no BranchID either
// - products are org-wide, same reasoning as CreateProductRequest. CategoryID
// is re-verified against the caller's own org by the service if present.
// Every field is a plain pointer, so (like every other Update*Request in
// this codebase) there's no way to distinguish "omitted" from "explicitly
// sent as null" - a nil Modifier always means "leave it as-is," never
// "clear it back to NULL"; clearing an existing Modifier isn't supported yet.
type UpdateProductRequest struct {
	CategoryID *uint            `json:"category_id" binding:"omitempty"`
	Name       *string          `json:"name" binding:"omitempty"`
	Barcode    *string          `json:"barcode" binding:"omitempty"`
	Price      *decimal.Decimal `json:"price" binding:"omitempty"`
	// DiscountPercent (0-100) is the only way to change a product's
	// discount; sale_price and the per-unit discount follow from it.
	DiscountPercent *decimal.Decimal `json:"discount_percent" binding:"omitempty"`
	// TaxPercent (0-100) is the only way to change a product's tax; the
	// per-unit tax follows from it and the sale price.
	TaxPercent *decimal.Decimal `json:"tax_percent" binding:"omitempty"`
	Threshold  *int             `json:"threshold" binding:"omitempty"`
	// CostPrice lets an Owner set the cost basis directly - useful for
	// stock that was never received through a real Procurement goods
	// receipt (imported starting inventory, catalog seeded before
	// Procurement was used) - see Product.CostPrice's own doc.
	CostPrice *decimal.Decimal `json:"cost_price" binding:"omitempty"`
	IsActive  *bool            `json:"is_active" binding:"omitempty"`
	Modifier  *string          `json:"modifier" binding:"omitempty"`
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
	// PriceSource says whose price/discount_percent/sale_price this is when
	// the product was resolved for a branch: "branch" (that branch has an
	// override) or "catalog". Absent when no branch was given - the plain
	// catalog row.
	PriceSource string `json:"price_source,omitempty"`
}

// SetBranchPriceRequest is the body of `PUT /products/:id/branch-prices/:branchId`.
// The PUT replaces the branch's whole override: a field sent as null (or left
// out) inherits the catalog value. Both null clears the override, same as
// DELETE. Price must be > 0 and DiscountPercent within 0-100, like the
// product's own - the service enforces it (decimal has no binding tags).
type SetBranchPriceRequest struct {
	Price           *decimal.Decimal `json:"price"`
	DiscountPercent *decimal.Decimal `json:"discount_percent"`
}

// BranchPriceActor is who writes a branch price: an org-wide account (UserID,
// no branch) or a staff member at a till (StaffID, plus the POS token's
// BranchID, which bounds what they may change). Exactly one of UserID / StaffID
// is set; BranchID is set exactly when StaffID is.
type BranchPriceActor struct {
	UserID   *uint
	StaffID  *uint
	BranchID *uint
}

// BranchPriceResult is one branch's row of `GET /products/:id/branch-prices`
// (and the response of the PUT). Price / DiscountPercent are the override
// itself - null where the branch inherits the catalog value; Effective* are
// what the branch actually charges. CatalogPrice / CatalogDiscountPercent are
// the product's own org-wide values - what a blank override field falls back
// to - because a till token never reads them elsewhere (GET /products is
// always resolved for its branch). UpdatedAt is null for a branch with no
// override.
type BranchPriceResult struct {
	BranchID                 uint             `json:"branch_id"`
	BranchName               string           `json:"branch_name"`
	CatalogPrice             decimal.Decimal  `json:"catalog_price"`
	CatalogDiscountPercent   decimal.Decimal  `json:"catalog_discount_percent"`
	Price                    *decimal.Decimal `json:"price"`
	DiscountPercent          *decimal.Decimal `json:"discount_percent"`
	EffectivePrice           decimal.Decimal  `json:"effective_price"`
	EffectiveDiscountPercent decimal.Decimal  `json:"effective_discount_percent"`
	EffectiveSalePrice       decimal.Decimal  `json:"effective_sale_price"`
	PriceSource              string           `json:"price_source"`
	UpdatedAt                *time.Time       `json:"updated_at"`
}

// ComboImageResult is one image attached to a combo, as returned by
// ListCombos. Same reasoning as ProductImageResult - URL is a temporary
// signed link, not the raw StorageKey.
type ComboImageResult struct {
	ID     uint   `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ComboItemResult is one bundled product of a combo as returned by
// ListCombos / the offline catalog snapshot - just what a till needs to
// expand the combo into one sale line per component (WORKFLOWS section 8),
// not the ComboItem row's own id/combo_id.
type ComboItemResult struct {
	ProductID uint `json:"product_id"`
	Qty       int  `json:"qty"`
}

// ComboResult is what ListCombos actually returns - the Combo row plus its
// component items and images, same reasoning as ProductResult. Items is
// never null; every combo has at least one (see CreateComboRequest).
type ComboResult struct {
	Combo
	Items  []ComboItemResult  `json:"items"`
	Images []ComboImageResult `json:"images"`
}
