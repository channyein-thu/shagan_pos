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
func (r *RepositoryImpl) ListSales(ctx context.Context, orgID uint, f SaleFilter) ([]Sale, int64, error) {
	base := r.db.WithContext(ctx).Model(&Sale{}).Where("org_id = ?", orgID)
	if f.BranchID != nil {
		base = base.Where("branch_id = ?", *f.BranchID)
	}
	if f.StaffID != nil {
		base = base.Where("staff_id = ?", *f.StaffID)
	}
	if f.ShiftID != nil {
		base = base.Where("shift_id = ?", *f.ShiftID)
	}
	if f.Start != nil {
		base = base.Where("completed_at >= ?", *f.Start)
	}
	if f.End != nil {
		base = base.Where("completed_at < ?", *f.End)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var sales []Sale
	// id breaks completed_at ties so pages never repeat or skip a row.
	err := base.Session(&gorm.Session{}).
		Order("completed_at DESC, id DESC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Find(&sales).Error
	if err != nil {
		return nil, 0, err
	}
	return sales, total, nil
}

// ListPaymentMethods backs the payment_methods field on `GET /sales` rows.
func (r *RepositoryImpl) ListPaymentMethods(ctx context.Context, saleIDs []uuid.UUID) (map[uuid.UUID][]PaymentMethod, error) {
	out := make(map[uuid.UUID][]PaymentMethod, len(saleIDs))
	if len(saleIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SaleID uuid.UUID
		Method PaymentMethod
	}
	err := r.db.WithContext(ctx).Table("payments").
		Select("DISTINCT sale_id, method").
		Where("sale_id IN ?", saleIDs).
		Order("sale_id, method").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.SaleID] = append(out[row.SaleID], row.Method)
	}
	return out, nil
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

// CreateDiscountApproval backs `POST /sales/discount-approvals`. Plain insert.
func (r *RepositoryImpl) CreateDiscountApproval(db *gorm.DB, approval *DiscountApproval) error {
	return db.Create(approval).Error
}

// ListDiscountApprovals backs CreateSale's line validation.
func (r *RepositoryImpl) ListDiscountApprovals(ctx context.Context, orgID uint, ids []uuid.UUID) ([]DiscountApproval, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var approvals []DiscountApproval
	if err := r.db.WithContext(ctx).Where("org_id = ? AND id IN ?", orgID, ids).Find(&approvals).Error; err != nil {
		return nil, err
	}
	return approvals, nil
}

// GetDiscountApproval backs `DELETE /sales/discount-approvals/:id`.
func (r *RepositoryImpl) GetDiscountApproval(ctx context.Context, orgID uint, id uuid.UUID) (*DiscountApproval, error) {
	var approval DiscountApproval
	if err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&approval).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("discount approval not found")
		}
		return nil, err
	}
	return &approval, nil
}

// MarkDiscountApprovalUsed spends an approval. The WHERE on status is the whole
// point: two sales racing to use the same approval both pass the read-side
// check, but only one UPDATE finds it still `approved`.
func (r *RepositoryImpl) MarkDiscountApprovalUsed(db *gorm.DB, id uuid.UUID, saleID uuid.UUID) (bool, error) {
	res := db.Model(&DiscountApproval{}).
		Where("id = ? AND status = ?", id, DiscountApprovalApproved).
		Updates(map[string]any{"status": DiscountApprovalUsed, "sale_id": saleID})
	return res.RowsAffected == 1, res.Error
}

// CancelDiscountApproval withdraws an approval that hasn't been spent.
func (r *RepositoryImpl) CancelDiscountApproval(db *gorm.DB, id uuid.UUID) (bool, error) {
	res := db.Model(&DiscountApproval{}).
		Where("id = ? AND status = ?", id, DiscountApprovalApproved).
		Update("status", DiscountApprovalCancelled)
	return res.RowsAffected == 1, res.Error
}

// FindOpenShift resolves the open shift of the till behind posUserID by table
// name - see Repository.FindOpenShift. The POS account carries the till's
// branch and device; the shift is that device's most recent open one.
func (r *RepositoryImpl) FindOpenShift(ctx context.Context, orgID uint, posUserID uint) (*OpenShift, error) {
	db := r.db.WithContext(ctx)
	var users []struct {
		BranchID *uint
		DeviceID *uint
	}
	if err := db.Table("users").
		Select("branch_id, device_id").
		Where("id = ? AND org_id = ? AND account_type = ?", posUserID, orgID, "pos").
		Limit(1).
		Scan(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 || users[0].BranchID == nil || users[0].DeviceID == nil {
		return nil, nil
	}
	var shifts []OpenShift
	if err := db.Table("shifts").
		Select("shifts.id AS id, shifts.branch_id AS branch_id").
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
	return &shifts[0], nil
}
