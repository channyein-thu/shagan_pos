package sales

import (
	"context"

	"github.com/google/uuid"
)

// Interface defines the sales domain's use cases.
type Interface interface {
	CreateSale(ctx context.Context, in Sale) (*Sale, error)
	ListSales(ctx context.Context) ([]Sale, error)
	GetSale(ctx context.Context, id uuid.UUID) (*Sale, error)
	GetSaleReceipt(ctx context.Context, id uuid.UUID) (map[string]any, error)
	ReprintSale(ctx context.Context, id uuid.UUID) (map[string]any, error)
	CreateHeldSale(ctx context.Context, in HeldSale) (*HeldSale, error)
	ListHeldSales(ctx context.Context) ([]HeldSale, error)
	ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) CreateSale(ctx context.Context, in Sale) (*Sale, error) {
	return s.repo.CreateSale(ctx, in)
}

func (s *Service) ListSales(ctx context.Context) ([]Sale, error) {
	return s.repo.ListSales(ctx)
}

func (s *Service) GetSale(ctx context.Context, id uuid.UUID) (*Sale, error) {
	return s.repo.GetSale(ctx, id)
}

func (s *Service) GetSaleReceipt(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	return s.repo.GetSaleReceipt(ctx, id)
}

func (s *Service) ReprintSale(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	return s.repo.ReprintSale(ctx, id)
}

func (s *Service) CreateHeldSale(ctx context.Context, in HeldSale) (*HeldSale, error) {
	return s.repo.CreateHeldSale(ctx, in)
}

func (s *Service) ListHeldSales(ctx context.Context) ([]HeldSale, error) {
	return s.repo.ListHeldSales(ctx)
}

func (s *Service) ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error) {
	return s.repo.ResumeHeldSale(ctx, id)
}
