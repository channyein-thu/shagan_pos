package returns

import (
	"context"
	"errors"

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

func (r *RepositoryImpl) CreateVoid(db *gorm.DB, v *Void) error {
	return db.Create(v).Error
}

func (r *RepositoryImpl) ListVoids(ctx context.Context, branchIDs []uint) ([]Void, error) {
	var voids []Void
	err := r.db.WithContext(ctx).
		Table("voids").
		Select("voids.*").
		Joins("JOIN sales ON sales.id = voids.sale_id").
		Where("sales.branch_id IN (?)", branchIDs).
		Order("voids.created_at DESC, voids.id DESC").
		Find(&voids).Error
	if err != nil {
		return nil, err
	}
	return voids, nil
}

func (r *RepositoryImpl) SaleHasReturnOrExchange(db *gorm.DB, saleID uuid.UUID) (bool, error) {
	var returnCount int64
	if err := db.Table("returns").Where("sale_id = ?", saleID).Count(&returnCount).Error; err != nil {
		return false, err
	}
	if returnCount > 0 {
		return true, nil
	}
	var exchangeCount int64
	if err := db.Table("exchanges").Where("sale_id = ?", saleID).Count(&exchangeCount).Error; err != nil {
		return false, err
	}
	return exchangeCount > 0, nil
}

func (r *RepositoryImpl) CreateReturn(db *gorm.DB, ret *Return) error {
	return db.Create(ret).Error
}

func (r *RepositoryImpl) CreateReturnItems(db *gorm.DB, items []ReturnItem) error {
	return db.Create(&items).Error
}

func (r *RepositoryImpl) ListReturns(ctx context.Context, branchIDs []uint) ([]Return, error) {
	var returnsList []Return
	err := r.db.WithContext(ctx).
		Table("returns").
		Select("returns.*").
		Joins("JOIN sales ON sales.id = returns.sale_id").
		Where("sales.branch_id IN (?)", branchIDs).
		Order("returns.created_at DESC, returns.id DESC").
		Find(&returnsList).Error
	if err != nil {
		return nil, err
	}
	return returnsList, nil
}

func (r *RepositoryImpl) GetReturn(ctx context.Context, branchIDs []uint, id uint) (*Return, error) {
	var ret Return
	err := r.db.WithContext(ctx).
		Table("returns").
		Select("returns.*").
		Joins("JOIN sales ON sales.id = returns.sale_id").
		Where("returns.id = ? AND sales.branch_id IN (?)", id, branchIDs).
		First(&ret).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("return not found")
		}
		return nil, err
	}
	return &ret, nil
}

func (r *RepositoryImpl) ReturnedQtyForSaleItem(db *gorm.DB, saleItemID uint) (int, error) {
	var total int
	err := db.Table("return_items").
		Select("COALESCE(SUM(qty), 0)").
		Where("sale_item_id = ?", saleItemID).
		Scan(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

func (r *RepositoryImpl) ExchangedInQtyForSaleItem(db *gorm.DB, saleItemID uint) (int, error) {
	var total int
	err := db.Table("exchange_items").
		Select("COALESCE(SUM(qty), 0)").
		Where("sale_item_id = ? AND direction = ?", saleItemID, DirectionIn).
		Scan(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

func (r *RepositoryImpl) CreateExchange(db *gorm.DB, e *Exchange) error {
	return db.Create(e).Error
}

func (r *RepositoryImpl) CreateExchangeItems(db *gorm.DB, items []ExchangeItem) error {
	return db.Create(&items).Error
}

func (r *RepositoryImpl) ListExchanges(ctx context.Context, branchIDs []uint) ([]Exchange, error) {
	var exchanges []Exchange
	err := r.db.WithContext(ctx).
		Table("exchanges").
		Select("exchanges.*").
		Joins("JOIN sales ON sales.id = exchanges.sale_id").
		Where("sales.branch_id IN (?)", branchIDs).
		Order("exchanges.created_at DESC, exchanges.id DESC").
		Find(&exchanges).Error
	if err != nil {
		return nil, err
	}
	return exchanges, nil
}

func (r *RepositoryImpl) GetExchange(ctx context.Context, branchIDs []uint, id uint) (*Exchange, error) {
	var e Exchange
	err := r.db.WithContext(ctx).
		Table("exchanges").
		Select("exchanges.*").
		Joins("JOIN sales ON sales.id = exchanges.sale_id").
		Where("exchanges.id = ? AND sales.branch_id IN (?)", id, branchIDs).
		First(&e).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("exchange not found")
		}
		return nil, err
	}
	return &e, nil
}
