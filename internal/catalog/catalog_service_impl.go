package catalog

import "context"

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

func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.repo.ListCategories(ctx)
}

func (s *Service) CreateCategory(ctx context.Context, in CreateCategoryRequest) (*Category, error) {
	return s.repo.CreateCategory(ctx, in)
}

func (s *Service) UpdateCategory(ctx context.Context, id uint, in UpdateCategoryRequest) (*Category, error) {
	return s.repo.UpdateCategory(ctx, id, in)
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
