package returns

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// VoidSale backs `POST /sales/:id/void`. Full or partial
func (r *Repository) VoidSale(ctx context.Context, id uuid.UUID, in Void) (*Void, error) {
	return nil, common.ErrNotImplemented
}

// ListVoids backs `GET /voids`.
func (r *Repository) ListVoids(ctx context.Context) ([]Void, error) {
	return nil, common.ErrNotImplemented
}

// CreateReturn backs `POST /returns`. Also writes return_items
func (r *Repository) CreateReturn(ctx context.Context, in Return) (*Return, error) {
	return nil, common.ErrNotImplemented
}

// ListReturns backs `GET /returns`.
func (r *Repository) ListReturns(ctx context.Context) ([]Return, error) {
	return nil, common.ErrNotImplemented
}

// GetReturn backs `GET /returns/:id`.
func (r *Repository) GetReturn(ctx context.Context, id uint) (*Return, error) {
	return nil, common.ErrNotImplemented
}

// CreateExchange backs `POST /exchanges`. Also writes exchange_items
func (r *Repository) CreateExchange(ctx context.Context, in Exchange) (*Exchange, error) {
	return nil, common.ErrNotImplemented
}

// ListExchanges backs `GET /exchanges`.
func (r *Repository) ListExchanges(ctx context.Context) ([]Exchange, error) {
	return nil, common.ErrNotImplemented
}

// GetExchange backs `GET /exchanges/:id`.
func (r *Repository) GetExchange(ctx context.Context, id uint) (*Exchange, error) {
	return nil, common.ErrNotImplemented
}
