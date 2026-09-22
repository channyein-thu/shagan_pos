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
	UpdateProduct(ctx context.Context, id uint, in UpdateProductRequest) (*Product, error)
	DeleteProduct(ctx context.Context, id uint) error
	ListCategories(ctx context.Context, orgID uint) ([]Category, error)
	CreateCategory(ctx context.Context, orgID uint, in CreateCategoryRequest) (*Category, error)
	UpdateCategory(ctx context.Context, orgID uint, id uint, in UpdateCategoryRequest) (*Category, error)
	DeleteCategory(ctx context.Context, orgID uint, id uint) error
	UploadMedia(ctx context.Context) (*ProductImage, error)
	ListCombos(ctx context.Context, orgID uint) ([]Combo, error)
	// CreateCombo requires at least one item (see CreateComboRequest) - a
	// combo without any bundled products should never exist. Each item's
	// ProductID must belong to orgID (not-found-not-forbidden, same as
	// CreateProduct's branch/category ownership checks). Unlike
	// CreateProduct, file is optional - pass nil (with fileSize 0 and empty
	// contentType/filename) when the request didn't include an image; a
	// combo can exist without one.
	CreateCombo(ctx context.Context, orgID uint, in CreateComboRequest, file io.ReadSeeker, fileSize int64, contentType, filename string) (*Combo, error)
	UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error)
	DeleteCombo(ctx context.Context, id uint) error
}
