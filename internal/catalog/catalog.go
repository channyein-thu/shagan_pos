package catalog

import "context"

// Interface defines the catalog domain's use cases.
type Interface interface {
	ListProducts(ctx context.Context) ([]Product, error)
	GetProduct(ctx context.Context, id uint) (*Product, error)
	GetProductByBarcode(ctx context.Context, code string) (*Product, error)
	CreateProduct(ctx context.Context, in Product) (*Product, error)
	UpdateProduct(ctx context.Context, id uint, in Product) (*Product, error)
	DeleteProduct(ctx context.Context, id uint) error
	ListCategories(ctx context.Context) ([]Category, error)
	CreateCategory(ctx context.Context, in Category) (*Category, error)
	UpdateCategory(ctx context.Context, id uint, in Category) (*Category, error)
	DeleteCategory(ctx context.Context, id uint) error
	UploadMedia(ctx context.Context) (*ProductImage, error)
	ListCombos(ctx context.Context) ([]Combo, error)
	CreateCombo(ctx context.Context, in Combo) (*Combo, error)
	UpdateCombo(ctx context.Context, id uint, in Combo) (*Combo, error)
	DeleteCombo(ctx context.Context, id uint) error
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
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

func (s *Service) CreateProduct(ctx context.Context, in Product) (*Product, error) {
	return s.repo.CreateProduct(ctx, in)
}

func (s *Service) UpdateProduct(ctx context.Context, id uint, in Product) (*Product, error) {
	return s.repo.UpdateProduct(ctx, id, in)
}

func (s *Service) DeleteProduct(ctx context.Context, id uint) error {
	return s.repo.DeleteProduct(ctx, id)
}

func (s *Service) ListCategories(ctx context.Context) ([]Category, error) {
	return s.repo.ListCategories(ctx)
}

func (s *Service) CreateCategory(ctx context.Context, in Category) (*Category, error) {
	return s.repo.CreateCategory(ctx, in)
}

func (s *Service) UpdateCategory(ctx context.Context, id uint, in Category) (*Category, error) {
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

func (s *Service) CreateCombo(ctx context.Context, in Combo) (*Combo, error) {
	return s.repo.CreateCombo(ctx, in)
}

func (s *Service) UpdateCombo(ctx context.Context, id uint, in Combo) (*Combo, error) {
	return s.repo.UpdateCombo(ctx, id, in)
}

func (s *Service) DeleteCombo(ctx context.Context, id uint) error {
	return s.repo.DeleteCombo(ctx, id)
}
