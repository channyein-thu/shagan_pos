package catalog

import (
	"context"

	"shagan_pos/internal/common"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListProducts(ctx context.Context) ([]Product, error) {
	return s.repo.ListProducts(ctx)
}

func (s *Service) GetProduct(ctx context.Context, id uint) (*Product, error) {
	return s.repo.GetProduct(ctx, id)
}

func (s *Service) GetProductByBarcode(ctx context.Context, code string) (*Product, error) {
	return s.repo.GetProductByBarcode(ctx, code)
}

func (s *Service) CreateProduct(ctx context.Context, in CreateProductRequest) (*Product, error) {
	return s.repo.CreateProduct(ctx, in)
}

func (s *Service) UpdateProduct(ctx context.Context, id uint, in UpdateProductRequest) (*Product, error) {
	return s.repo.UpdateProduct(ctx, id, in)
}

func (s *Service) DeleteProduct(ctx context.Context, id uint) error {
	return s.repo.DeleteProduct(ctx, id)
}

func (s *Service) ListCategories(ctx context.Context, orgID uint) ([]Category, error) {
	return s.repo.ListCategories(ctx, orgID)
}

// CreateCategory returns common.ConflictError if org_id+name_i18n collides
// with an existing category (see the ux_categories_org_name unique index on
// Category) - the repository just reports whatever error the database gives
// it; deciding what that error means to the caller is the service's job.
func (s *Service) CreateCategory(ctx context.Context, orgID uint, in CreateCategoryRequest) (*Category, error) {
	category := Category{
		OrgID:    orgID,
		NameI18n: in.NameI18n,
	}
	if err := s.repo.CreateCategory(ctx, &category); err != nil {
		if common.IsDuplicateError(err) {
			return nil, common.ConflictError("a category with this name already exists")
		}
		return nil, err
	}
	return &category, nil
}

// UpdateCategory confirms the category exists AND belongs to orgID before
// touching anything (same not-found-not-forbidden reasoning as
// identity.UpdateBranch), decides which fields actually changed, and returns
// common.ConflictError if the new name collides with another category in the
// same org.
func (s *Service) UpdateCategory(ctx context.Context, orgID uint, id uint, in UpdateCategoryRequest) (*Category, error) {
	if _, err := s.repo.GetCategory(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.NameI18n != nil {
		updates["name_i18n"] = *in.NameI18n
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateCategory(ctx, id, updates); err != nil {
			if common.IsDuplicateError(err) {
				return nil, common.ConflictError("a category with this name already exists")
			}
			return nil, err
		}
	}

	return s.repo.GetCategory(ctx, orgID, id)
}

func (s *Service) DeleteCategory(ctx context.Context, id uint) error {
	return s.repo.DeleteCategory(ctx, id)
}

func (s *Service) UploadMedia(ctx context.Context) (*ProductImage, error) {
	return s.repo.UploadMedia(ctx)
}

func (s *Service) ListCombos(ctx context.Context) ([]Combo, error) {
	return s.repo.ListCombos(ctx)
}

func (s *Service) CreateCombo(ctx context.Context, in CreateComboRequest) (*Combo, error) {
	return s.repo.CreateCombo(ctx, in)
}

func (s *Service) UpdateCombo(ctx context.Context, id uint, in UpdateComboRequest) (*Combo, error) {
	return s.repo.UpdateCombo(ctx, id, in)
}

func (s *Service) DeleteCombo(ctx context.Context, id uint) error {
	return s.repo.DeleteCombo(ctx, id)
}
