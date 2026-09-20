package catalog

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// ListProducts backs `GET /products`, scoped to orgID (Product carries its
// own OrgID directly, so no join through branches is needed, unlike
// identity.ListStaff).
func (r *RepositoryImpl) ListProducts(ctx context.Context, orgID uint, branchID *uint) ([]Product, error) {
	var products []Product
	q := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if err := q.Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

// GetProduct backs `GET /products/:id`.
func (r *RepositoryImpl) GetProduct(ctx context.Context, id uint) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// GetProductByBarcode backs `GET /products/barcode/:code`. Exact-match scan lookup
func (r *RepositoryImpl) GetProductByBarcode(ctx context.Context, code string) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// CreateProduct backs Service.CreateProduct's first step. Plain insert -
// whatever error the database gives back (including a unique-constraint
// violation on ux_products_branch_barcode) is returned as-is; interpreting
// it is the service's job, same reasoning as CreateCategory.
func (r *RepositoryImpl) CreateProduct(db *gorm.DB, product *Product) error {
	return db.Create(product).Error
}

// CreateProductImage backs Service.CreateProduct's second step. Plain insert.
func (r *RepositoryImpl) CreateProductImage(db *gorm.DB, image *ProductImage) error {
	return db.Create(image).Error
}

// UpdateProduct backs `PATCH /products/:id`.
func (r *RepositoryImpl) UpdateProduct(ctx context.Context, id uint, in UpdateProductRequest) (*Product, error) {
	return nil, common.ErrNotImplemented
}

// DeleteProduct backs `DELETE /products/:id`. Soft delete only
func (r *RepositoryImpl) DeleteProduct(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}

// ListCategories backs `GET /categories`, scoped to orgID.
func (r *RepositoryImpl) ListCategories(ctx context.Context, orgID uint) ([]Category, error) {
	var categories []Category
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Find(&categories).Error; err != nil {
		return nil, err
	}
	return categories, nil
}

// CreateCategory backs Service.CreateCategory. Plain insert - whatever error
// the database gives back (including a unique-constraint violation on
// ux_categories_org_name) is returned as-is; interpreting it is the
// service's job, not this one's.
func (r *RepositoryImpl) CreateCategory(ctx context.Context, category *Category) error {
	return r.db.WithContext(ctx).Create(category).Error
}

// GetCategory backs Service.UpdateCategory's existence/ownership check.
func (r *RepositoryImpl) GetCategory(ctx context.Context, orgID uint, id uint) (*Category, error) {
	var category Category
	err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&category).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("category not found")
		}
		return nil, err
	}
	return &category, nil
}

// UpdateCategory backs `PATCH /categories/:id`. Plain write - updates is
// already decided by the service; whatever error the database gives back
// (including a unique-constraint violation on ux_categories_org_name) is
// returned as-is, same reasoning as CreateCategory.
func (r *RepositoryImpl) UpdateCategory(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&Category{}).Where("id = ?", id).Updates(updates).Error
}

// ProductsExistForCategory backs Service.DeleteCategory's
// referential-integrity check.
func (r *RepositoryImpl) ProductsExistForCategory(ctx context.Context, categoryID uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&Product{}).Where("category_id = ?", categoryID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteCategory backs `DELETE /categories/:id`. Hard delete.
func (r *RepositoryImpl) DeleteCategory(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&Category{}, id).Error
}

// UploadMedia backs `POST /media`. Upload; returns storage_key
func (r *RepositoryImpl) UploadMedia(ctx context.Context) (*ProductImage, error) {
	return nil, common.ErrNotImplemented
}

// ListCombos backs `GET /combos`.
func (r *RepositoryImpl) ListCombos(ctx context.Context) ([]Combo, error) {
	return nil, common.ErrNotImplemented
}

// CreateCombo backs `POST /combos`.
func (r *RepositoryImpl) CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error) {
	return nil, common.ErrNotImplemented
}

// UpdateCombo backs `PATCH /combos/:id`.
func (r *RepositoryImpl) UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error) {
	return nil, common.ErrNotImplemented
}

// DeleteCombo backs `DELETE /combos/:id`.
func (r *RepositoryImpl) DeleteCombo(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}
