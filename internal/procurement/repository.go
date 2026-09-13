package procurement

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListSuppliers backs `GET /suppliers`.
func (r *Repository) ListSuppliers(ctx context.Context) ([]Supplier, error) {
	return nil, common.ErrNotImplemented
}

// CreateSupplier backs `POST /suppliers`.
func (r *Repository) CreateSupplier(ctx context.Context, in Supplier) (*Supplier, error) {
	return nil, common.ErrNotImplemented
}

// UpdateSupplier backs `PATCH /suppliers/:id`.
func (r *Repository) UpdateSupplier(ctx context.Context, id uint, in Supplier) (*Supplier, error) {
	return nil, common.ErrNotImplemented
}

// DeleteSupplier backs `DELETE /suppliers/:id`.
func (r *Repository) DeleteSupplier(ctx context.Context, id uint) error {
	return common.ErrNotImplemented
}

// ListPurchaseOrders backs `GET /purchase-orders`.
func (r *Repository) ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// CreatePurchaseOrder backs `POST /purchase-orders`. Also writes purchase_order_items
func (r *Repository) CreatePurchaseOrder(ctx context.Context, in PurchaseOrder) (*PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// GetPurchaseOrder backs `GET /purchase-orders/:id`.
func (r *Repository) GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// UpdatePurchaseOrder backs `PATCH /purchase-orders/:id`. Status transitions
func (r *Repository) UpdatePurchaseOrder(ctx context.Context, id uint, in PurchaseOrder) (*PurchaseOrder, error) {
	return nil, common.ErrNotImplemented
}

// CreateGoodsReceipt backs `POST /purchase-orders/:id/receipts`. Posting increases stock + writes ledger
func (r *Repository) CreateGoodsReceipt(ctx context.Context, id uint, in GoodsReceipt) (*GoodsReceipt, error) {
	return nil, common.ErrNotImplemented
}
