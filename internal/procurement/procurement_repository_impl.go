package procurement

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

// ListSuppliers backs `GET /suppliers`.
func (r *RepositoryImpl) ListSuppliers(ctx context.Context) ([]Supplier, error) {
	return nil, common.ErrNotImplemented
}

// CreateSupplier backs `POST /suppliers`.
func (r *RepositoryImpl) CreateSupplier(ctx context.Context, in CreateSupplierRequest) (*Supplier, error) {
	return nil, common.ErrNotImplemented
}

// UpdateSupplier backs `PATCH /suppliers/:id`.
func (r *RepositoryImpl) UpdateSupplier(ctx context.Context, id uint, in UpdateSupplierRequest) (*Supplier, error) {
	return nil, common.ErrNotImplemented
}

// DeleteSupplier backs `DELETE /suppliers/:id`.
func (r *RepositoryImpl) DeleteSupplier(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
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
