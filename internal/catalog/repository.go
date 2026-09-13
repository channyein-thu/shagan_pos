package catalog

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListProducts backs `GET /products`. List/search/filter
func (r *Repository) ListProducts(ctx context.Context) ([]Product, error) {
	return nil, common.ErrNotImplemented
}

// GetProduct backs `GET /products/:id`.
func (r *Repository) GetProduct(ctx context.Context, id uint) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// GetProductByBarcode backs `GET /products/barcode/:code`. Exact-match scan lookup
func (r *Repository) GetProductByBarcode(ctx context.Context, code string) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// CreateProduct backs `POST /products`.
func (r *Repository) CreateProduct(ctx context.Context, in Product) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// UpdateProduct backs `PATCH /products/:id`.
func (r *Repository) UpdateProduct(ctx context.Context, id uint, in Product) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// DeleteProduct backs `DELETE /products/:id`. Soft delete only
func (r *Repository) DeleteProduct(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}

// ListCategories backs `GET /categories`.
func (r *Repository) ListCategories(ctx context.Context) ([]Category, error) {
	return nil, common.ErrNotImplemented
}

// CreateCategory backs `POST /categories`.
func (r *Repository) CreateCategory(ctx context.Context, in Category) (*Category, error) {
	return nil, common.ErrNotImplemented
}

// UpdateCategory backs `PATCH /categories/:id`.
func (r *Repository) UpdateCategory(ctx context.Context, id uint, in Category) (*Category, error) {
	return nil, common.ErrNotImplemented
}

// DeleteCategory backs `DELETE /categories/:id`.
func (r *Repository) DeleteCategory(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}

// UploadMedia backs `POST /media`. Upload; returns storage_key
func (r *Repository) UploadMedia(ctx context.Context) (*ProductImage, error) {
	return nil, common.ErrNotImplemented
}

// ListCombos backs `GET /combos`.
func (r *Repository) ListCombos(ctx context.Context) ([]Combo, error) {
	return nil, common.ErrNotImplemented
}

// CreateCombo backs `POST /combos`.
func (r *Repository) CreateCombo(ctx context.Context, in Combo) (*Combo, error) {
	return nil, common.ErrNotImplemented
}

// UpdateCombo backs `PATCH /combos/:id`.
func (r *Repository) UpdateCombo(ctx context.Context, id uint, in Combo) (*Combo, error) {
	return nil, common.ErrNotImplemented
}

// DeleteCombo backs `DELETE /combos/:id`.
func (r *Repository) DeleteCombo(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}
