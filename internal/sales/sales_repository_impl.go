package sales

import (
	"context"
	"errors"

	"github.com/google/uuid"
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

// RequireOpenShift joins to branches (owned by the identity domain) to
// confirm shiftID belongs to branchID within orgID and is currently open -
// same cross-domain-via-table-name approach as shift.Expense's own
// branch-ownership check.
func (r *RepositoryImpl) RequireOpenShift(db *gorm.DB, orgID uint, branchID uint, shiftID uint) error {
	var count int64
	err := db.Table("shifts").
		Joins("JOIN branches ON branches.id = shifts.branch_id").
		Where("shifts.id = ? AND shifts.branch_id = ? AND branches.org_id = ? AND shifts.status = ?", shiftID, branchID, orgID, "open").
		Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return common.NotFoundError("shift not found, not open, or doesn't belong to this branch")
	}
	return nil
}

// CreateSale backs `POST /sales`. Plain insert.
func (r *RepositoryImpl) CreateSale(db *gorm.DB, sale *Sale) error {
	return db.Create(sale).Error
}

// CreateSaleItems backs `POST /sales`. Plain bulk insert.
func (r *RepositoryImpl) CreateSaleItems(db *gorm.DB, items []SaleItem) error {
	return db.Create(&items).Error
}

// CreatePayments backs `POST /sales`. Plain bulk insert.
func (r *RepositoryImpl) CreatePayments(db *gorm.DB, payments []Payment) error {
	return db.Create(&payments).Error
}

// ListSales backs `GET /sales`.
func (r *RepositoryImpl) ListSales(ctx context.Context, orgID uint) ([]Sale, error) {
	var sales []Sale
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("completed_at DESC").Find(&sales).Error; err != nil {
		return nil, err
	}
	return sales, nil
}

// GetSale backs `GET /sales/:id`.
func (r *RepositoryImpl) GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*Sale, error) {
	var sale Sale
	err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&sale).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("sale not found")
		}
		return nil, err
	}
	return &sale, nil
}

// ListSaleItems backs the receipt bundle behind `GET /sales/:id/receipt` and
// `POST /sales/:id/reprint`.
func (r *RepositoryImpl) ListSaleItems(ctx context.Context, saleID uuid.UUID) ([]SaleItem, error) {
	var items []SaleItem
	if err := r.db.WithContext(ctx).Where("sale_id = ?", saleID).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetSaleWithLock is GetSale's transaction-participating, row-locking
// counterpart - see the Repository interface doc.
func (r *RepositoryImpl) GetSaleWithLock(db *gorm.DB, orgID uint, id uuid.UUID) (*Sale, error) {
	var sale Sale
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND org_id = ?", id, orgID).First(&sale).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("sale not found")
		}
		return nil, err
	}
	return &sale, nil
}

// ListSaleItemsTx is ListSaleItems's transaction-participating counterpart -
// see the Repository interface doc.
func (r *RepositoryImpl) ListSaleItemsTx(db *gorm.DB, saleID uuid.UUID) ([]SaleItem, error) {
	var items []SaleItem
	if err := db.Where("sale_id = ?", saleID).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// UpdateSaleStatus backs returns.Service.VoidSale. Plain field write.
func (r *RepositoryImpl) UpdateSaleStatus(db *gorm.DB, id uuid.UUID, status SaleStatus) error {
	return db.Model(&Sale{}).Where("id = ?", id).Update("status", status).Error
}

// ListPayments backs the receipt bundle behind `GET /sales/:id/receipt` and
// `POST /sales/:id/reprint`.
func (r *RepositoryImpl) ListPayments(ctx context.Context, saleID uuid.UUID) ([]Payment, error) {
	var payments []Payment
	if err := r.db.WithContext(ctx).Where("sale_id = ?", saleID).Find(&payments).Error; err != nil {
		return nil, err
	}
	return payments, nil
}

// CreateHeldSale backs `POST /held-sales`. Plain insert.
func (r *RepositoryImpl) CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error) {
	held := HeldSale{
		BranchID:    in.BranchID,
		StaffID:     in.StaffID,
		CustomerRef: in.CustomerRef,
		Items:       in.Items,
		Discount:    in.Discount,
		HeldAt:      in.HeldAt,
	}
	if err := r.db.WithContext(ctx).Create(&held).Error; err != nil {
		return nil, err
	}
	return &held, nil
}

// ListHeldSales backs `GET /held-sales`. Branch-scoped, not staff-scoped -
// see the Repository interface doc comment.
func (r *RepositoryImpl) ListHeldSales(ctx context.Context, branchID uint) ([]HeldSale, error) {
	var held []HeldSale
	if err := r.db.WithContext(ctx).Where("branch_id = ?", branchID).Order("held_at DESC").Find(&held).Error; err != nil {
		return nil, err
	}
	return held, nil
}

// ResumeHeldSale backs `DELETE /held-sales/:id`. Atomic delete-and-restore -
// locks the row so two concurrent resume attempts can't both succeed.
func (r *RepositoryImpl) ResumeHeldSale(ctx context.Context, branchID uint, id uint) (*HeldSale, error) {
	var held HeldSale
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND branch_id = ?", id, branchID).
			First(&held).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.NotFoundError("held sale not found")
			}
			return err
		}
		return tx.Delete(&held).Error
	})
	if err != nil {
		return nil, err
	}
	return &held, nil
}
