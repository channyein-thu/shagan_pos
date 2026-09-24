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
	// GetProductByBarcode backs `GET /products/barcode/:code`. Scoped to both
	// orgID and branchID - Barcode is only unique per branch
	// (ux_products_branch_barcode), not per org, so without a branch a
	// barcode could match more than one product across an org's branches.
	// Same not-found-not-forbidden reasoning as GetProduct.
	GetProductByBarcode(ctx context.Context, orgID uint, branchID uint, code string) (*Product, error)
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
	// UpdateProduct applies updates (already decided by the service - which
	// fields changed, in what shape) to the product identified by id. Plain
	// write - existence/ownership was already confirmed by a prior
	// GetProduct call, same reasoning as UpdateCategory. Unlike
	// UpdateCategory, db is either the repository's normal connection or an
	// in-flight transaction handed down by the caller - Service.UpdateProduct
	// runs this and, when the request includes a new image,
	// DeleteProductImagesByProductID/CreateProductImage inside one
	// db.Transaction, same reasoning as CreateProduct/CreateProductImage.
	UpdateProduct(db *gorm.DB, id uint, updates map[string]any) error
	// DeleteProductImagesByProductID backs Service.UpdateProduct's
	// image-replace step - removes the product's existing image row(s)
	// before the new one is created. Plain delete, same
	// db-is-either-plain-or-in-flight-transaction reasoning as CreateProduct.
	DeleteProductImagesByProductID(db *gorm.DB, productID uint) error
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
	// ListCombos backs `GET /combos`, scoped to the authenticated caller's
	// own organization - same reasoning as identity's org-scoped lists
	// (e.g. ListBranches).
	ListCombos(ctx context.Context, orgID uint) ([]Combo, error)
	// GetCombo backs Service.UpdateCombo's existence/ownership check.
	// Scoped to orgID - returns common.NotFoundError for a combo that
	// exists but belongs to a different org, same as one that doesn't exist
	// at all, so a caller can never distinguish "not mine" from "doesn't
	// exist" by probing IDs (same reasoning as GetCategory/GetProduct).
	GetCombo(ctx context.Context, orgID uint, id uint) (*Combo, error)
	// CreateCombo backs Service.CreateCombo's first step. Plain insert -
	// GORM sets the row's ID on the pointer it's given. Mapping the
	// request/orgID into a Combo, and validating it, happens in the
	// service, not here. db is either the repository's normal connection or
	// an in-flight transaction handed down by the caller -
	// Service.CreateCombo runs this and CreateComboItems inside one
	// db.Transaction, since a combo without any bundled products should
	// never exist (same reasoning as Product/ProductImage).
	CreateCombo(db *gorm.DB, combo *Combo) error
	// CreateComboItems backs Service.CreateCombo's second step. Plain
	// slice-insert, same db-is-either-plain-or-in-flight-transaction
	// reasoning as CreateCombo above.
	CreateComboItems(db *gorm.DB, items []ComboItem) error
	// CreateComboImage backs Service.CreateCombo's optional third step -
	// only called when the request actually included an image. Plain
	// insert, same db-is-either-plain-or-in-flight-transaction reasoning as
	// CreateCombo above.
	CreateComboImage(db *gorm.DB, image *ComboImage) error
	// ListComboImagesByComboID backs Service.UpdateCombo's image-replace
	// step - a plain query, fetched before the transaction only to know
	// what to clean up from storage after a successful commit.
	ListComboImagesByComboID(ctx context.Context, comboID uint) ([]ComboImage, error)
	// UpdateCombo applies updates (already decided by the service - which
	// fields changed, in what shape) to the combo identified by id. Plain
	// write - existence/ownership was already confirmed by a prior
	// GetCombo call, same reasoning as UpdateCategory/UpdateProduct. db is
	// either the repository's normal connection or an in-flight
	// transaction handed down by the caller - Service.UpdateCombo runs
	// this and, when the request includes a new image,
	// DeleteComboImagesByComboID/CreateComboImage inside one
	// db.Transaction, same reasoning as UpdateProduct.
	UpdateCombo(db *gorm.DB, id uint, updates map[string]any) error
	// DeleteComboImagesByComboID backs Service.UpdateCombo's image-replace
	// step - removes the combo's existing image row(s) before the new one
	// is created. Plain delete, same
	// db-is-either-plain-or-in-flight-transaction reasoning as UpdateCombo.
	DeleteComboImagesByComboID(db *gorm.DB, comboID uint) error
	DeleteCombo(ctx context.Context, id uint) error
}
