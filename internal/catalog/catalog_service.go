package catalog

import (
	"context"
	"io"

	"shagan_pos/internal/identity"
)

// BranchLookup is the one identity operation catalog needs: confirming a
// client-supplied BranchID actually belongs to the caller's org before a
// Product gets attached to it. identity.Repository already satisfies this
// signature - no adapter needed, see NewCatalogAPI's wiring.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
}

// Interface defines the catalog domain's use cases.
type Interface interface {
	// ListProducts/GetProduct return ProductResult, not bare Product - each
	// result's Images carry a temporary signed URL, not just a StorageKey,
	// since the bucket is private and a raw key isn't usable by a client.
	ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]ProductResult, error)
	GetProduct(ctx context.Context, orgID uint, id uint) (*ProductResult, error)
	// GetProductByBarcode requires a branchID for the same reason as the
	// repository method it calls - a barcode is only unique per branch, so
	// resolving one org-wide could be ambiguous. Returns ProductResult, same
	// reasoning as GetProduct.
	GetProductByBarcode(ctx context.Context, orgID uint, branchID uint, code string) (*ProductResult, error)
	// CreateProduct requires exactly one image at creation time - a product
	// without one should never exist (see Service.CreateProduct). file must
	// support Seek: its header gets read once to decode Width/Height, then
	// rewound before the full content is uploaded to object storage.
	CreateProduct(ctx context.Context, orgID uint, in CreateProductRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Product, error)
	// UpdateProduct confirms the product exists AND belongs to orgID before
	// touching anything (not-found-not-forbidden, same reasoning as
	// UpdateCategory). BranchID/CategoryID, if present, are re-verified
	// against orgID same as CreateProduct. Price/Discount/Tax are validated
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
	UploadMedia(ctx context.Context) (*ProductImage, error)
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
	DeleteCombo(ctx context.Context, id uint) error
}
