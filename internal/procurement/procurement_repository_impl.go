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

// ListPurchaseOrders backs `GET /purchase-orders`.
func (r *RepositoryImpl) ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// CreatePurchaseOrder backs `POST /purchase-orders`. Also writes purchase_order_items
func (r *RepositoryImpl) CreatePurchaseOrder(ctx context.Context, in CreatePurchaseOrderRequest) (*PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// GetPurchaseOrder backs `GET /purchase-orders/:id`.
func (r *RepositoryImpl) GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// UpdatePurchaseOrder backs `PATCH /purchase-orders/:id`. Status transitions
func (r *RepositoryImpl) UpdatePurchaseOrder(ctx context.Context, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// CreateGoodsReceipt backs `POST /purchase-orders/:id/receipts`. Posting increases stock + writes ledger
func (r *RepositoryImpl) CreateGoodsReceipt(ctx context.Context, id uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error) {
	return nil, common.ErrNotImplemented
}
