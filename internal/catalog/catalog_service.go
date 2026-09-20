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
	ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]Product, error)
	GetProduct(ctx context.Context, orgID uint, id uint) (*Product, error)
	GetProductByBarcode(ctx context.Context, code string) (*Product, error)
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
	ListCombos(ctx context.Context) ([]Combo, error)
	CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error)
	UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error)
	DeleteCombo(ctx context.Context, id uint) error
}
