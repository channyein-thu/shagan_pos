package sales

import (
	"context"

	"github.com/google/uuid"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) CreateSale(ctx context.Context, in CreateSaleRequest) (*Sale, error) {
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

func (s *Service) CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error) {
	return s.repo.CreateHeldSale(ctx, in)
}

func (s *Service) ListHeldSales(ctx context.Context) ([]HeldSale, error) {
	return s.repo.ListHeldSales(ctx)
}

func (s *Service) ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error) {
	return s.repo.ResumeHeldSale(ctx, id)
}
