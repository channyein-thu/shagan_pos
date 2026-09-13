package sales

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

// CreateSale backs `POST /sales`. Idempotent, transactional; also decrements stock + writes ledger
func (r *Repository) CreateSale(ctx context.Context, in Sale) (*Sale, error) {
	return nil, common.ErrNotImplemented
}

// ListSales backs `GET /sales`.
func (r *Repository) ListSales(ctx context.Context) ([]Sale, error) {
	return nil, common.ErrNotImplemented
}

// GetSale backs `GET /sales/:id`.
func (r *Repository) GetSale(ctx context.Context, id uuid.UUID) (*Sale, error) {
	return nil, common.ErrNotImplemented
}

// GetSaleReceipt backs `GET /sales/:id/receipt`. ESC/POS payload
func (r *Repository) GetSaleReceipt(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// ReprintSale backs `POST /sales/:id/reprint`.
func (r *Repository) ReprintSale(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	return nil, common.ErrNotImplemented
}

// CreateHeldSale backs `POST /held-sales`.
func (r *Repository) CreateHeldSale(ctx context.Context, in HeldSale) (*HeldSale, error) {
	return nil, common.ErrNotImplemented
}

// ListHeldSales backs `GET /held-sales`.
func (r *Repository) ListHeldSales(ctx context.Context) ([]HeldSale, error) {
	return nil, common.ErrNotImplemented
}

// ResumeHeldSale backs `DELETE /held-sales/:id`. Resume - atomic delete-and-restore
func (r *Repository) ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error) {
	return nil, common.ErrNotImplemented
}
