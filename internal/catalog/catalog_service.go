package catalog

import (
	"context"
	"io"
)

// Interface defines the catalog domain's use cases.
type Interface interface {
	// ListProducts/GetProduct return ProductResult, not bare Product - each
	// result's Images carry a temporary signed URL, not just a StorageKey,
	// since the bucket is private and a raw key isn't usable by a client.
	// Products are org-wide, not branch-scoped, so there's no branch filter.
	ListProducts(ctx context.Context, orgID uint) ([]ProductResult, error)
	GetProduct(ctx context.Context, orgID uint, id uint) (*ProductResult, error)
	// GetProductByBarcode is scoped to orgID only - Barcode is unique per
	// org (ux_products_org_barcode). Returns ProductResult, same reasoning
	// as GetProduct.
	GetProductByBarcode(ctx context.Context, orgID uint, code string) (*ProductResult, error)
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
	// CreateProduct's image handling.
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
	// reasoning as UpdateProduct's image replace.
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
