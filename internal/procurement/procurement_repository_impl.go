package procurement

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

// ListSuppliers backs `GET /suppliers`, scoped to orgID.
func (r *RepositoryImpl) ListSuppliers(ctx context.Context, orgID uint) ([]Supplier, error) {
	var suppliers []Supplier
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Find(&suppliers).Error; err != nil {
		return nil, err
	}
	return suppliers, nil
}

// CreateSupplier backs Service.CreateSupplier. Plain insert.
func (r *RepositoryImpl) CreateSupplier(ctx context.Context, supplier *Supplier) error {
	return r.db.WithContext(ctx).Create(supplier).Error
}

// GetSupplier backs Service.UpdateSupplier/DeleteSupplier's
// existence/ownership check.
func (r *RepositoryImpl) GetSupplier(ctx context.Context, orgID uint, id uint) (*Supplier, error) {
	var supplier Supplier
	err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&supplier).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("supplier not found")
		}
		return nil, err
	}
	return &supplier, nil
}

// UpdateSupplier backs `PATCH /suppliers/:id`. Plain write - updates is
// already decided by the service.
func (r *RepositoryImpl) UpdateSupplier(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&Supplier{}).Where("id = ?", id).Updates(updates).Error
}

// PurchaseOrdersExistForSupplier backs Service.DeleteSupplier's
// referential-integrity check.
func (r *RepositoryImpl) PurchaseOrdersExistForSupplier(ctx context.Context, supplierID uint) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&PurchaseOrder{}).Where("supplier_id = ?", supplierID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// DeleteSupplier backs `DELETE /suppliers/:id`. Hard delete.
func (r *RepositoryImpl) DeleteSupplier(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&Supplier{}, id).Error
}

// orgSupplierIDs backs ListPurchaseOrders/GetPurchaseOrder's org-scoping -
// PurchaseOrder carries no OrgID of its own, only SupplierID, so this
// mirrors identity's orgBranchIDs: a same-domain subquery, not a
// cross-domain dependency (Supplier lives in this same package).
func (r *RepositoryImpl) orgSupplierIDs(ctx context.Context, orgID uint) *gorm.DB {
	return r.db.WithContext(ctx).Model(&Supplier{}).Where("org_id = ?", orgID).Select("id")
}

// ListPurchaseOrders backs `GET /purchase-orders`, scoped to orgID.
func (r *RepositoryImpl) ListPurchaseOrders(ctx context.Context, orgID uint) ([]PurchaseOrder, error) {
	var orders []PurchaseOrder
	if err := r.db.WithContext(ctx).Where("supplier_id IN (?)", r.orgSupplierIDs(ctx, orgID)).Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

// CountPurchaseOrders backs Service.CreatePurchaseOrder's po_number
// generation. Scoped directly by PurchaseOrder's own org_id column, unlike
// ListPurchaseOrders/GetPurchaseOrder's supplier-subquery scoping above.
func (r *RepositoryImpl) CountPurchaseOrders(db *gorm.DB, orgID uint) (int64, error) {
	var count int64
	if err := db.Model(&PurchaseOrder{}).Where("org_id = ?", orgID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CreatePurchaseOrder backs Service.CreatePurchaseOrder's first step. Plain
// insert.
func (r *RepositoryImpl) CreatePurchaseOrder(db *gorm.DB, po *PurchaseOrder) error {
	return db.Create(po).Error
}

// CreatePurchaseOrderItems backs Service.CreatePurchaseOrder's second step.
// Plain slice-insert.
func (r *RepositoryImpl) CreatePurchaseOrderItems(db *gorm.DB, items []PurchaseOrderItem) error {
	return db.Create(&items).Error
}

// GetPurchaseOrder backs `GET /purchase-orders/:id`, scoped to orgID.
func (r *RepositoryImpl) GetPurchaseOrder(ctx context.Context, orgID uint, id uint) (*PurchaseOrder, error) {
	var po PurchaseOrder
	err := r.db.WithContext(ctx).
		Where("id = ? AND supplier_id IN (?)", id, r.orgSupplierIDs(ctx, orgID)).
		First(&po).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("purchase order not found")
		}
		return nil, err
	}
	return &po, nil
}

// ListPurchaseOrderItemsByPoID backs Service.GetPurchaseOrder/
// Service.CreateGoodsReceipt's item lookups.
func (r *RepositoryImpl) ListPurchaseOrderItemsByPoID(ctx context.Context, poID uint) ([]PurchaseOrderItem, error) {
	var items []PurchaseOrderItem
	if err := r.db.WithContext(ctx).Where("po_id = ?", poID).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// UpdatePurchaseOrder backs `PATCH /purchase-orders/:id`. Plain write -
// updates is already decided by the service.
func (r *RepositoryImpl) UpdatePurchaseOrder(ctx context.Context, id uint, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&PurchaseOrder{}).Where("id = ?", id).Updates(updates).Error
}

// UpdatePurchaseOrderStatus backs Service.CreateGoodsReceipt's final step.
// Plain write.
func (r *RepositoryImpl) UpdatePurchaseOrderStatus(db *gorm.DB, id uint, status PurchaseOrderStatus) error {
	return db.Model(&PurchaseOrder{}).Where("id = ?", id).Update("status", status).Error
}

// CreateGoodsReceipt backs Service.CreateGoodsReceipt's first step. Plain
// insert.
func (r *RepositoryImpl) CreateGoodsReceipt(db *gorm.DB, receipt *GoodsReceipt) error {
	return db.Create(receipt).Error
}

// CreateGoodsReceiptItems backs Service.CreateGoodsReceipt's second step.
// Plain slice-insert.
func (r *RepositoryImpl) CreateGoodsReceiptItems(db *gorm.DB, items []GoodsReceiptItem) error {
	return db.Create(&items).Error
}
