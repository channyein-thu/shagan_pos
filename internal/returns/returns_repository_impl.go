package returns

import (
	"context"

	"github.com/google/uuid"
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

// VoidSale backs `POST /sales/:id/void`. Full or partial
func (r *RepositoryImpl) VoidSale(ctx context.Context, id uuid.UUID, in VoidSaleRequest) (*Void, error) {
	return nil, common.ErrNotImplemented
}

// ListVoids backs `GET /voids`.
func (r *RepositoryImpl) ListVoids(ctx context.Context) ([]Void, error) {
	return nil, common.ErrNotImplemented
}

// CreateReturn backs `POST /returns`. Also writes return_items
func (r *RepositoryImpl) CreateReturn(ctx context.Context, in CreateReturnRequest) (*Return, error) {
	return nil, common.ErrNotImplemented
}

// ListReturns backs `GET /returns`.
func (r *RepositoryImpl) ListReturns(ctx context.Context) ([]Return, error) {
	return nil, common.ErrNotImplemented
}

// GetReturn backs `GET /returns/:id`.
func (r *RepositoryImpl) GetReturn(ctx context.Context, id uint) (*Return, error) {
	return nil, common.ErrNotImplemented
}

// CreateExchange backs `POST /exchanges`. Also writes exchange_items
func (r *RepositoryImpl) CreateExchange(ctx context.Context, in CreateExchangeRequest) (*Exchange, error) {
	return nil, common.ErrNotImplemented
}

// ListExchanges backs `GET /exchanges`.
func (r *RepositoryImpl) ListExchanges(ctx context.Context) ([]Exchange, error) {
	return nil, common.ErrNotImplemented
}

// GetExchange backs `GET /exchanges/:id`.
func (r *RepositoryImpl) GetExchange(ctx context.Context, id uint) (*Exchange, error) {
	return nil, common.ErrNotImplemented
}
