package inventory

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

// ListLowStock backs `GET /inventory/low-stock`. Joins to catalog's own
// products table (cross-domain via table name, not a Go import - same
// approach as shift.Expense's branch-ownership check) to keep only rows
// where the current qty has fallen to or below that product's own
// reorder threshold.
func (r *RepositoryImpl) ListLowStock(ctx context.Context, branchIDs []uint) ([]StockLevel, error) {
	var levels []StockLevel
	err := r.db.WithContext(ctx).
		Model(&StockLevel{}).
		Select("stock_levels.*").
		Joins("JOIN products ON products.id = stock_levels.product_id").
		Where("stock_levels.branch_id IN (?) AND stock_levels.qty <= products.threshold", branchIDs).
		Find(&levels).Error
	if err != nil {
		return nil, err
	}
	return levels, nil
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

// CreateStockAdjustment backs `POST /inventory/adjustments`. Plain insert.
func (r *RepositoryImpl) CreateStockAdjustment(db *gorm.DB, adjustment *StockAdjustment) error {
	return db.Create(adjustment).Error
}

// ListStockTransfers backs `GET /stock-transfers`, scoped to branchIDs
// (already resolved by the service to the caller's own org) - matches
// either FromBranch or ToBranch.
func (r *RepositoryImpl) ListStockTransfers(ctx context.Context, branchIDs []uint) ([]StockTransfer, error) {
	var transfers []StockTransfer
	err := r.db.WithContext(ctx).
		Where("from_branch IN (?) OR to_branch IN (?)", branchIDs, branchIDs).
		Order("created_at DESC, id DESC").
		Find(&transfers).Error
	if err != nil {
		return nil, err
	}
	return transfers, nil
}

// GetStockTransfer backs UpdateStockTransfer's existence/ownership check.
func (r *RepositoryImpl) GetStockTransfer(db *gorm.DB, branchIDs []uint, id uint, lock bool) (*StockTransfer, error) {
	q := db.Where("id = ? AND (from_branch IN (?) OR to_branch IN (?))", id, branchIDs, branchIDs)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var transfer StockTransfer
	if err := q.First(&transfer).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("stock transfer not found")
		}
		return nil, err
	}
	return &transfer, nil
}

// ListStockTransferItems backs the completing-a-transfer path.
func (r *RepositoryImpl) ListStockTransferItems(db *gorm.DB, transferID uint) ([]StockTransferItem, error) {
	var items []StockTransferItem
	if err := db.Where("transfer_id = ?", transferID).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// CreateStockTransfer backs `POST /stock-transfers`. Plain insert.
func (r *RepositoryImpl) CreateStockTransfer(db *gorm.DB, transfer *StockTransfer) error {
	return db.Create(transfer).Error
}

// CreateStockTransferItems backs `POST /stock-transfers`. Plain bulk insert.
func (r *RepositoryImpl) CreateStockTransferItems(db *gorm.DB, items []StockTransferItem) error {
	return db.Create(&items).Error
}

// UpdateStockTransferStatus backs every status transition. Plain field write.
func (r *RepositoryImpl) UpdateStockTransferStatus(db *gorm.DB, id uint, status TransferStatus) error {
	return db.Model(&StockTransfer{}).Where("id = ?", id).Update("status", status).Error
}
