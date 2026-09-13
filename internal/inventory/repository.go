package inventory

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListStockLevels backs `GET /stock-levels`. Filter by product/branch
func (r *Repository) ListStockLevels(ctx context.Context) ([]StockLevel, error) {
	return nil, common.ErrNotImplemented
}

// ListLowStock backs `GET /inventory/low-stock`.
func (r *Repository) ListLowStock(ctx context.Context) ([]StockLevel, error) {
	return nil, common.ErrNotImplemented
}

// ListInventoryLedger backs `GET /inventory/ledger`. Read-only - never written directly by a client
func (r *Repository) ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error) {
	return nil, common.ErrNotImplemented
}

// CreateStockAdjustment backs `POST /inventory/adjustments`. Writes a ledger row as a side effect
func (r *Repository) CreateStockAdjustment(ctx context.Context, in StockAdjustment) (*StockAdjustment, error) {
	return nil, common.ErrNotImplemented
}

// ListStockTransfers backs `GET /stock-transfers`.
func (r *Repository) ListStockTransfers(ctx context.Context) ([]StockTransfer, error) {
	return nil, common.ErrNotImplemented
}

// CreateStockTransfer backs `POST /stock-transfers`. Also writes stock_transfers_items
func (r *Repository) CreateStockTransfer(ctx context.Context, in StockTransfer) (*StockTransfer, error) {
	return nil, common.ErrNotImplemented
}

// UpdateStockTransfer backs `PATCH /stock-transfers/:id`. Status lifecycle: pending -> in-transit -> received
func (r *Repository) UpdateStockTransfer(ctx context.Context, id uint, in StockTransfer) (*StockTransfer, error) {
	return nil, common.ErrNotImplemented
}
