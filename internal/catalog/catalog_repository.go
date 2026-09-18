package catalog

import "context"

// Repository defines the catalog domain's persistence operations.
type Repository interface {
	ListProducts(ctx context.Context) ([]Product, error)
	GetProduct(ctx context.Context, id uint) (*Product, error)
	GetProductByBarcode(ctx context.Context, code string) (*Product, error)
	CreateProduct(ctx context.Context, in CreateProductRequest) (*Product, error)
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
	DeleteCategory(ctx context.Context, id uint) error
	UploadMedia(ctx context.Context) (*ProductImage, error)
	ListCombos(ctx context.Context) ([]Combo, error)
	CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error)
	UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error)
	DeleteCombo(ctx context.Context, id uint) error
}
