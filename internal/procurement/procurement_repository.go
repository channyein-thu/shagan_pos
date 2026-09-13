package procurement

import "context"

// Repository defines the procurement domain's persistence operations.
type Repository interface {
	ListSuppliers(ctx context.Context) ([]Supplier, error)
	CreateSupplier(ctx context.Context, in CreateSupplierRequest) (*Supplier, error)
	UpdateSupplier(ctx context.Context, id uint, in UpdateSupplierRequest) (*Supplier, error)
	DeleteSupplier(ctx context.Context, id uint) error
	ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error)
	CreatePurchaseOrder(ctx context.Context, in CreatePurchaseOrderRequest) (*PurchaseOrder, error)
	GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error)
	UpdatePurchaseOrder(ctx context.Context, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error)
	CreateGoodsReceipt(ctx context.Context, id uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error)
}
