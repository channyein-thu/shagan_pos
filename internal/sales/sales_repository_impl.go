package sales

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

// CreateSale backs `POST /sales`. Idempotent, transactional; also decrements stock + writes ledger
func (r *RepositoryImpl) CreateSale(ctx context.Context, in CreateSaleRequest) (*Sale, error) {
	return nil, common.ErrNotImplemented
}

// ListSales backs `GET /sales`.
func (r *RepositoryImpl) ListSales(ctx context.Context) ([]Sale, error) {
	return nil, common.ErrNotImplemented
}

// GetSale backs `GET /sales/:id`.
func (r *RepositoryImpl) GetSale(ctx context.Context, id uuid.UUID) (*Sale, error) {
	return nil, common.ErrNotImplemented
}

// GetSaleReceipt backs `GET /sales/:id/receipt`. ESC/POS payload
func (r *RepositoryImpl) GetSaleReceipt(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ReprintSale backs `POST /sales/:id/reprint`.
func (r *RepositoryImpl) ReprintSale(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// CreateHeldSale backs `POST /held-sales`.
func (r *RepositoryImpl) CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error) {
	return nil, common.ErrNotImplemented
}

// ListHeldSales backs `GET /held-sales`.
func (r *RepositoryImpl) ListHeldSales(ctx context.Context) ([]HeldSale, error) {
	return nil, common.ErrNotImplemented
}

// ResumeHeldSale backs `DELETE /held-sales/:id`. Resume - atomic delete-and-restore
func (r *RepositoryImpl) ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error) {
	return nil, common.ErrNotImplemented
}
