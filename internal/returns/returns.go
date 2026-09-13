package returns

import (
	"context"

	"github.com/google/uuid"
)

// Interface defines the returns domain's use cases.
type Interface interface {
	VoidSale(ctx context.Context, id uuid.UUID, in Void) (*Void, error)
	ListVoids(ctx context.Context) ([]Void, error)
	CreateReturn(ctx context.Context, in Return) (*Return, error)
	ListReturns(ctx context.Context) ([]Return, error)
	GetReturn(ctx context.Context, id uint) (*Return, error)
	CreateExchange(ctx context.Context, in Exchange) (*Exchange, error)
	ListExchanges(ctx context.Context) ([]Exchange, error)
	GetExchange(ctx context.Context, id uint) (*Exchange, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) VoidSale(ctx context.Context, id uuid.UUID, in Void) (*Void, error) {
	return s.repo.VoidSale(ctx, id, in)
}

func (s *Service) ListVoids(ctx context.Context) ([]Void, error) {
	return s.repo.ListVoids(ctx)
}

func (s *Service) CreateReturn(ctx context.Context, in Return) (*Return, error) {
	return s.repo.CreateReturn(ctx, in)
}

func (s *Service) ListReturns(ctx context.Context) ([]Return, error) {
	return s.repo.ListReturns(ctx)
}

func (s *Service) GetReturn(ctx context.Context, id uint) (*Return, error) {
	return s.repo.GetReturn(ctx, id)
}

func (s *Service) CreateExchange(ctx context.Context, in Exchange) (*Exchange, error) {
	return s.repo.CreateExchange(ctx, in)
}

func (s *Service) ListExchanges(ctx context.Context) ([]Exchange, error) {
	return s.repo.ListExchanges(ctx)
}

func (s *Service) GetExchange(ctx context.Context, id uint) (*Exchange, error) {
	return s.repo.GetExchange(ctx, id)
}
