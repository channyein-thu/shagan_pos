package catalog

import (
	"context"

	"gorm.io/gorm"
)

// Repository defines the catalog domain's persistence operations.
type Repository interface {
	// ListProducts backs `GET /products`, scoped to the authenticated
	// caller's own organization - same reasoning as identity's org-scoped
	// lists. branchID additionally restricts to one branch when set - the
	// caller's own branch (from a pos-device token), never a client-supplied
	// ID, same reasoning as identity.ListStaff.
	ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]Product, error)
	// GetProduct backs `GET /products/:id`. Scoped to orgID - returns
	// common.NotFoundError for a product that exists but belongs to a
	// different org, same as one that doesn't exist at all, so a caller can
	// never distinguish "not mine" from "doesn't exist" by probing IDs (same
	// reasoning as identity's GetBranch/GetStaff). Not further restricted to
	// the caller's own branch - same as identity.GetStaff.
	GetProduct(ctx context.Context, orgID uint, id uint) (*Product, error)
	GetProductByBarcode(ctx context.Context, code string) (*Product, error)
	// CreateProduct backs Service.CreateProduct's first step. Plain insert -
	// GORM sets the row's ID on the pointer it's given. Mapping the
	// request/orgID into a Product, and validating it, happens in the
	// service, not here. db is either the repository's normal connection or
	// an in-flight transaction handed down by the caller -
	// Service.CreateProduct runs this and CreateProductImage inside one
	// db.Transaction, since a product row without its required image should
	// never exist.
	CreateProduct(db *gorm.DB, product *Product) error
	// CreateProductImage backs Service.CreateProduct's second step. Plain
	// insert, same db-is-either-plain-or-in-flight-transaction reasoning as
	// CreateProduct above.
	CreateProductImage(db *gorm.DB, image *ProductImage) error
	// ListProductImagesByProductIDs backs Service.ListProducts/GetProduct's
	// image lookup - a plain query, no business decision about which
	// products the caller is allowed to see (that's already been decided by
	// the ListProducts/GetProduct call that produced productIDs).
	ListProductImagesByProductIDs(ctx context.Context, productIDs []uint) ([]ProductImage, error)
	UpdateProduct(ctx context.Context, id uint, in UpdateProductRequest) (*Product, error)
	DeleteProduct(ctx context.Context, id uint) error
	// ListCategories backs `GET /categories`, scoped to the authenticated
	// caller's own organization - same reasoning as identity's
	// org-scoped lists (e.g. ListBranches).
	ListCategories(ctx context.Context, orgID uint) ([]Category, error)
	// CreateCategory backs Service.CreateCategory. Plain insert - GORM sets
	// the row's ID on the pointer it's given. Mapping the request/orgID into
	// a Category happens in the service, not here.
	CreateCategory(ctx context.Context, category *Category) error
	// GetCategory backs Service.UpdateCategory's existence/ownership check.
	// Scoped to orgID - returns common.NotFoundError for a category that
	// exists but belongs to a different org, same as one that doesn't exist
	// at all, so a caller can never distinguish "not mine" from "doesn't
	// exist" by probing IDs (same reasoning as identity's GetBranch).
	GetCategory(ctx context.Context, orgID uint, id uint) (*Category, error)
	// UpdateCategory applies updates (already decided by the service - which
	// fields changed, in what shape) to the category identified by id. Plain
	// write - existence/ownership was already confirmed by a prior
	// GetCategory call.
	UpdateCategory(ctx context.Context, id uint, updates map[string]any) error
	// ProductsExistForCategory backs Service.DeleteCategory's
	// referential-integrity check - a plain existence query. Whether that
	// should block the delete is the service's call, not this one's.
	ProductsExistForCategory(ctx context.Context, categoryID uint) (bool, error)
	// DeleteCategory is a hard delete - Category has no status field to
	// deactivate instead (unlike Staff/Branch/Device). Existence/ownership was
	// already confirmed by a prior GetCategory call.
	DeleteCategory(ctx context.Context, id uint) error
	UploadMedia(ctx context.Context) (*ProductImage, error)
	ListCombos(ctx context.Context) ([]Combo, error)
	CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error)
	UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error)
	DeleteCombo(ctx context.Context, id uint) error
}
