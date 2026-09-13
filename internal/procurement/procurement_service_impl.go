package procurement

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListSuppliers(ctx context.Context) ([]Supplier, error) {
	return s.repo.ListSuppliers(ctx)
}

func (s *Service) CreateSupplier(ctx context.Context, in CreateSupplierRequest) (*Supplier, error) {
	return s.repo.CreateSupplier(ctx, in)
}

func (s *Service) UpdateSupplier(ctx context.Context, id uint, in UpdateSupplierRequest) (*Supplier, error) {
	return s.repo.UpdateSupplier(ctx, id, in)
}

func (s *Service) DeleteSupplier(ctx context.Context, id uint) error {
	return s.repo.DeleteSupplier(ctx, id)
}

func (s *Service) ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error) {
	return s.repo.ListPurchaseOrders(ctx)
}

func (s *Service) CreatePurchaseOrder(ctx context.Context, in CreatePurchaseOrderRequest) (*PurchaseOrder, error) {
	return s.repo.CreatePurchaseOrder(ctx, in)
}

func (s *Service) GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error) {
	return s.repo.GetPurchaseOrder(ctx, id)
}

func (s *Service) UpdatePurchaseOrder(ctx context.Context, id uint, in UpdatePurchaseOrderRequest) (*PurchaseOrder, error) {
	return s.repo.UpdatePurchaseOrder(ctx, id, in)
}

func (s *Service) CreateGoodsReceipt(ctx context.Context, id uint, in CreateGoodsReceiptRequest) (*GoodsReceipt, error) {
	return s.repo.CreateGoodsReceipt(ctx, id, in)
}
