package returns

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/sales"
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

// CurrentShiftID backs the shift stamp on Return/Exchange.
func (r *RepositoryImpl) CurrentShiftID(db *gorm.DB, orgID uint, userID uint) (*uint, error) {
	var users []struct {
		BranchID *uint
		DeviceID *uint
	}
	if err := db.Table("users").
		Select("branch_id, device_id").
		Where("id = ? AND org_id = ? AND account_type = ?", userID, orgID, "pos").
		Limit(1).
		Scan(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 || users[0].BranchID == nil || users[0].DeviceID == nil {
		return nil, nil
	}

	var shifts []struct{ ID uint }
	if err := db.Table("shifts").
		Select("shifts.id").
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where("branches.org_id = ? AND shifts.branch_id = ? AND shifts.device_id = ? AND shifts.status = ?",
			orgID, *users[0].BranchID, *users[0].DeviceID, "open").
		Order("shifts.opened_at DESC").
		Limit(1).
		Scan(&shifts).Error; err != nil {
		return nil, err
	}
	if len(shifts) == 0 {
		return nil, nil
	}
	return &shifts[0].ID, nil
}

// ReturnedQtyBySaleItems backs the receipt's returned_qty/returnable_qty. It
// is the batch form of ReturnedQtyForSaleItem + ExchangedInQtyForSaleItem and
// must keep summing exactly what they sum (return lines, plus exchange lines
// with direction "in"), since those are what the over-return guards enforce.
func (r *RepositoryImpl) ReturnedQtyBySaleItems(ctx context.Context, saleItemIDs []uint) (map[uint]int, error) {
	out := make(map[uint]int, len(saleItemIDs))
	if len(saleItemIDs) == 0 {
		return out, nil
	}
	db := r.db.WithContext(ctx)
	type row struct {
		SaleItemID uint
		Qty        int
	}
	var returned []row
	if err := db.Table("return_items").
		Select("sale_item_id, COALESCE(SUM(qty), 0) AS qty").
		Where("sale_item_id IN ?", saleItemIDs).
		Group("sale_item_id").
		Scan(&returned).Error; err != nil {
		return nil, err
	}
	var exchangedIn []row
	if err := db.Table("exchange_items").
		Select("sale_item_id, COALESCE(SUM(qty), 0) AS qty").
		Where("sale_item_id IN ? AND direction = ?", saleItemIDs, DirectionIn).
		Group("sale_item_id").
		Scan(&exchangedIn).Error; err != nil {
		return nil, err
	}
	for _, rr := range returned {
		out[rr.SaleItemID] += rr.Qty
	}
	for _, rr := range exchangedIn {
		out[rr.SaleItemID] += rr.Qty
	}
	return out, nil
}

// ReturnSummaries backs has_return / has_exchange / refunded_total on the
// sales history rows and the receipt. refunded_total is the sum of the sale's
// Returns' refund_total - an Exchange settles a difference, it doesn't refund.
func (r *RepositoryImpl) ReturnSummaries(ctx context.Context, saleIDs []uuid.UUID) (map[uuid.UUID]sales.ReturnSummary, error) {
	out := make(map[uuid.UUID]sales.ReturnSummary, len(saleIDs))
	if len(saleIDs) == 0 {
		return out, nil
	}
	db := r.db.WithContext(ctx)
	var returned []struct {
		SaleID uuid.UUID
		Total  decimal.Decimal
	}
	if err := db.Table("returns").
		Select("sale_id, COALESCE(SUM(refund_total), 0) AS total").
		Where("sale_id IN ?", saleIDs).
		Group("sale_id").
		Scan(&returned).Error; err != nil {
		return nil, err
	}
	var exchanged []struct{ SaleID uuid.UUID }
	if err := db.Table("exchanges").
		Select("DISTINCT sale_id").
		Where("sale_id IN ?", saleIDs).
		Scan(&exchanged).Error; err != nil {
		return nil, err
	}
	for _, rr := range returned {
		out[rr.SaleID] = sales.ReturnSummary{HasReturn: true, RefundedTotal: rr.Total}
	}
	for _, rr := range exchanged {
		summary := out[rr.SaleID]
		summary.HasExchange = true
		out[rr.SaleID] = summary
	}
	return out, nil
}
