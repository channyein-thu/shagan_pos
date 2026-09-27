package inventory

import (
	"context"
	"errors"

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

// ListStockLevels backs `GET /stock-levels`, scoped to branchIDs (already
// resolved by the service to the caller's own org), optionally narrowed to
// one product.
func (r *RepositoryImpl) ListStockLevels(ctx context.Context, branchIDs []uint, productID *uint) ([]StockLevel, error) {
	var levels []StockLevel
	q := r.db.WithContext(ctx).Where("branch_id IN (?)", branchIDs)
	if productID != nil {
		q = q.Where("product_id = ?", *productID)
	}
	if err := q.Find(&levels).Error; err != nil {
		return nil, err
	}
	return levels, nil
}

// ListLowStock backs `GET /inventory/low-stock`.
func (r *RepositoryImpl) ListLowStock(ctx context.Context) ([]StockLevel, error) {
	return nil, common.ErrNotImplemented
}

// ListInventoryLedger backs `GET /inventory/ledger`, scoped to orgID and
// optionally narrowed to one branch/product. Read-only - never written
// directly by a client.
func (r *RepositoryImpl) ListInventoryLedger(ctx context.Context, orgID uint, branchID *uint, productID *uint) ([]InventoryLedger, error) {
	var entries []InventoryLedger
	q := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if productID != nil {
		q = q.Where("product_id = ?", *productID)
	}
	if err := q.Order("created_at ASC, id ASC").Find(&entries).Error; err != nil {
		return nil, err
	}
	return entries, nil
}

// GetStockLevel backs the find-or-create step for crediting stock.
func (r *RepositoryImpl) GetStockLevel(db *gorm.DB, productID uint, branchID uint) (*StockLevel, error) {
	var level StockLevel
	err := db.Where("product_id = ? AND branch_id = ?", productID, branchID).First(&level).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &level, nil
}

// CreateStockLevel backs the first-ever-stock-movement path. Plain insert.
func (r *RepositoryImpl) CreateStockLevel(db *gorm.DB, level *StockLevel) error {
	return db.Create(level).Error
}

// UpdateStockLevelQty backs the already-exists path. Plain write.
func (r *RepositoryImpl) UpdateStockLevelQty(db *gorm.DB, id uint, qty int) error {
	return db.Model(&StockLevel{}).Where("id = ?", id).Update("qty", qty).Error
}

// CreateInventoryLedgerEntry backs every audit-trail write in this domain.
// Plain insert.
func (r *RepositoryImpl) CreateInventoryLedgerEntry(db *gorm.DB, entry *InventoryLedger) error {
	return db.Create(entry).Error
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
