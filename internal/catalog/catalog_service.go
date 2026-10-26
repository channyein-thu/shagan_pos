package catalog

import (
	"context"
	"io"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/identity"
)

// BranchLookup is what catalog needs from identity: confirming a branch
// belongs to the caller's org (and is active, for a price write) and listing
// the org's branches for the per-branch price table. identity.Repository
// already satisfies this signature - no adapter needed.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
	ListBranches(ctx context.Context, orgID uint) ([]identity.Branch, error)
}

// AuditWriter is what catalog needs from audit: recording every branch-price
// upsert and delete. audit.Repository already satisfies this signature - no
// adapter needed.
type AuditWriter interface {
	CreateAuditLog(db *gorm.DB, entry *audit.AuditLog) error
}

// Interface defines the catalog domain's use cases.
type Interface interface {
	// ListProducts/GetProduct return ProductResult, not bare Product - each
	// result's Images carry a temporary signed URL, not just a StorageKey,
	// since the bucket is private and a raw key isn't usable by a client.
	// Products are org-wide; branchID, when non-nil, resolves each product's
	// price, discount_percent and sale_price for that branch (its override
	// where one exists, else the catalog value - see EffectivePricing) and
	// sets ProductResult.PriceSource. The branch must belong to orgID
	// (common.NotFoundError otherwise). nil = the plain catalog row.
	ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]ProductResult, error)
	GetProduct(ctx context.Context, orgID uint, id uint, branchID *uint) (*ProductResult, error)
	// GetProductByBarcode is scoped to orgID only - Barcode is unique per
	// org (ux_products_org_barcode). Returns ProductResult, same reasoning as
	// GetProduct, and resolves for branchID the same way.
	GetProductByBarcode(ctx context.Context, orgID uint, code string, branchID *uint) (*ProductResult, error)
	// ListBranchPrices backs `GET /products/:id/branch-prices`: one row per
	// branch of the org (or just branchID when non-nil) with that branch's
	// override and effective prices. The product must belong to orgID.
	ListBranchPrices(ctx context.Context, orgID uint, productID uint, branchID *uint) ([]BranchPriceResult, error)
	// SetBranchPrice backs `PUT /products/:id/branch-prices/:branchId` - an
	// upsert of the branch's whole override. The product and branch must
	// belong to orgID (not found otherwise) and the branch must be active
	// (400). Price must be > 0 and DiscountPercent within 0-100; both nil
	// clears the override (same as DeleteBranchPrice). sale_price is computed
	// here with the product's own rule. The write and its audit entry
	// (before / after, branch, the actor) are one transaction. An actor bound
	// to a branch (a manager at a till) may only write that branch - anything
	// else is a 403, checked before the product or branch is even looked up.
	SetBranchPrice(ctx context.Context, orgID uint, actor BranchPriceActor, productID uint, branchID uint, in SetBranchPriceRequest) (*BranchPriceResult, error)
	// DeleteBranchPrice backs `DELETE /products/:id/branch-prices/:branchId`:
	// the branch goes back to the catalog price. Idempotent - no override is
	// a no-op (and writes no audit entry). An inactive branch is allowed, so
	// a closed branch's overrides can still be cleaned up.
	DeleteBranchPrice(ctx context.Context, orgID uint, actor BranchPriceActor, productID uint, branchID uint) error
	// CreateProduct requires exactly one image at creation time - a product
	// without one should never exist (see Service.CreateProduct). file must
	// support Seek: its header gets read once to decode Width/Height, then
	// rewound before the full content is uploaded to object storage.
	CreateProduct(ctx context.Context, orgID uint, in CreateProductRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Product, error)
	// UpdateProduct confirms the product exists AND belongs to orgID before
	// touching anything (not-found-not-forbidden, same reasoning as
	// UpdateCategory). CategoryID, if present, is re-verified against orgID
	// same as CreateProduct. Price/Discount/Tax are validated
	// against the resulting combined state (existing values for any field
	// not present in the request), not just the fields actually being
	// changed. file is optional (nil when the request didn't include one) -
	// when present, it entirely replaces the product's existing image: the
	// old ProductImage row and its storage object are only removed after the
	// new one is successfully created, same failure-cleanup reasoning as
	// CreateProduct's image handling. A change to price or discount_percent
	// also recomputes the stored sale_price of every branch override that
	// inherits the changed field, in the same transaction.
	UpdateProduct(ctx context.Context, orgID uint, id uint, in UpdateProductRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Product, error)
	// DeleteProduct confirms the product exists AND belongs to orgID before
	// touching anything (not-found-not-forbidden, same reasoning as
	// UpdateProduct), then blocks the delete with common.ConflictError if
	// any combo still references it via ComboItem - deleting out from
	// under a combo would leave it bundling a product that no longer
	// exists, same reasoning as DeleteCategory. The product's own
	// ProductImage row(s) and their storage objects are removed as part of
	// the delete (they belong to the product, not a separate concern that
	// should block it) - the DB rows atomically with the product row, the
	// storage objects best-effort afterward, same cleanup-after-commit
	// reasoning as UpdateProduct's image replace. The product's branch price
	// overrides are deleted in the same transaction (no DB foreign keys).
	DeleteProduct(ctx context.Context, orgID uint, id uint) error
	ListCategories(ctx context.Context, orgID uint) ([]Category, error)
	CreateCategory(ctx context.Context, orgID uint, in CreateCategoryRequest) (*Category, error)
	UpdateCategory(ctx context.Context, orgID uint, id uint, in UpdateCategoryRequest) (*Category, error)
	DeleteCategory(ctx context.Context, orgID uint, id uint) error
	// ListCombos returns ComboResult, not bare Combo - each result's Images
	// carry a temporary signed URL, not just a StorageKey, same reasoning
	// as ListProducts.
	ListCombos(ctx context.Context, orgID uint) ([]ComboResult, error)
	// CreateCombo requires at least one item (see CreateComboRequest) - a
	// combo without any bundled products should never exist. Each item's
	// ProductID must belong to orgID (not-found-not-forbidden, same as
	// CreateProduct's branch/category ownership checks). Unlike
	// CreateProduct, file is optional - pass nil (with fileSize 0 and empty
	// contentType/filename) when the request didn't include an image; a
	// combo can exist without one.
	CreateCombo(ctx context.Context, orgID uint, in CreateComboRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Combo, error)
	// UpdateCombo confirms the combo exists AND belongs to orgID before
	// touching anything (not-found-not-forbidden, same reasoning as
	// UpdateCategory/UpdateProduct). Price/ExpiresAt, if present, are
	// validated against the resulting combined state (existing values for
	// any field not present in the request), same reasoning as
	// UpdateProduct. file is optional (nil when the request didn't include
	// one) - when present, it entirely replaces the combo's existing
	// image, same failure-cleanup reasoning as UpdateProduct's image
	// handling. Doesn't support editing Items yet.
	UpdateCombo(ctx context.Context, orgID uint, id uint, in UpdateComboRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Combo, error)
	// DeleteCombo confirms the combo exists AND belongs to orgID before
	// touching anything (not-found-not-forbidden, same reasoning as
	// UpdateCombo). Unlike DeleteProduct, there's currently nothing outside
	// the combo that can reference it (no Sales/order-history domain yet),
	// so there's no referential-integrity gate to check - only the combo's
	// own ComboItem/ComboImage rows and image storage object are cleaned
	// up as part of the delete, same reasoning as DeleteProduct's
	// ProductImage cleanup.
	DeleteCombo(ctx context.Context, orgID uint, id uint) error
}
