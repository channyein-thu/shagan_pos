package inventory

import (
	"context"

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

// ListStockLevels backs `GET /stock-levels`. Filter by product/branch
func (r *RepositoryImpl) ListStockLevels(ctx context.Context) ([]StockLevel, error) {
	return nil, common.ErrNotImplemented
}

// ListLowStock backs `GET /inventory/low-stock`.
func (r *RepositoryImpl) ListLowStock(ctx context.Context) ([]StockLevel, error) {
	return nil, common.ErrNotImplemented
}

// ListInventoryLedger backs `GET /inventory/ledger`. Read-only - never written directly by a client
func (r *RepositoryImpl) ListInventoryLedger(ctx context.Context) ([]InventoryLedger, error) {
	return nil, common.ErrNotImplemented
}

// CreateStockAdjustment backs `POST /inventory/adjustments`. Writes a ledger row as a side effect
func (r *RepositoryImpl) CreateStockAdjustment(ctx context.Context, in CreateStockAdjustmentRequest) (*StockAdjustment, error) {
	return nil, common.ErrNotImplemented
}

// ListStockTransfers backs `GET /stock-transfers`.
func (r *RepositoryImpl) ListStockTransfers(ctx context.Context) ([]StockTransfer, error) {
	return nil, common.ErrNotImplemented
}

// CreateStockTransfer backs `POST /stock-transfers`. Also writes stock_transfers_items
func (r *RepositoryImpl) CreateStockTransfer(ctx context.Context, in CreateStockTransferRequest) (*StockTransfer, error) {
	return nil, common.ErrNotImplemented
}

// UpdateStockTransfer backs `PATCH /stock-transfers/:id`. Status lifecycle: pending -> in-transit -> received
func (r *RepositoryImpl) UpdateStockTransfer(ctx context.Context, id uint, in UpdateStockTransferRequest) (*StockTransfer, error) {
	return nil, common.ErrNotImplemented
}
