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
	ListCategories(ctx context.Context) ([]Category, error)
	CreateCategory(ctx context.Context, in CreateCategoryRequest) (*Category, error)
	UpdateCategory(ctx context.Context, id uint, in UpdateCategoryRequest) (*Category, error)
	DeleteCategory(ctx context.Context, id uint) error
	UploadMedia(ctx context.Context) (*ProductImage, error)
	ListCombos(ctx context.Context) ([]Combo, error)
	CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error)
	UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error)
	DeleteCombo(ctx context.Context, id uint) error
}
