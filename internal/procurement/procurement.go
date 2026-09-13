package procurement

import "context"

// Interface defines the procurement domain's use cases.
type Interface interface {
	ListSuppliers(ctx context.Context) ([]Supplier, error)
	CreateSupplier(ctx context.Context, in Supplier) (*Supplier, error)
	UpdateSupplier(ctx context.Context, id uint, in Supplier) (*Supplier, error)
	DeleteSupplier(ctx context.Context, id uint) error
	ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error)
	CreatePurchaseOrder(ctx context.Context, in PurchaseOrder) (*PurchaseOrder, error)
	GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error)
	UpdatePurchaseOrder(ctx context.Context, id uint, in PurchaseOrder) (*PurchaseOrder, error)
	CreateGoodsReceipt(ctx context.Context, id uint, in GoodsReceipt) (*GoodsReceipt, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListSuppliers(ctx context.Context) ([]Supplier, error) {
	return s.repo.ListSuppliers(ctx)
}

func (s *Service) CreateSupplier(ctx context.Context, in Supplier) (*Supplier, error) {
	return s.repo.CreateSupplier(ctx, in)
}

func (s *Service) UpdateSupplier(ctx context.Context, id uint, in Supplier) (*Supplier, error) {
	return s.repo.UpdateSupplier(ctx, id, in)
}

func (s *Service) DeleteSupplier(ctx context.Context, id uint) error {
	return s.repo.DeleteSupplier(ctx, id)
}

func (s *Service) ListPurchaseOrders(ctx context.Context) ([]PurchaseOrder, error) {
	return s.repo.ListPurchaseOrders(ctx)
}

func (s *Service) CreatePurchaseOrder(ctx context.Context, in PurchaseOrder) (*PurchaseOrder, error) {
	return s.repo.CreatePurchaseOrder(ctx, in)
}

func (s *Service) GetPurchaseOrder(ctx context.Context, id uint) (*PurchaseOrder, error) {
	return s.repo.GetPurchaseOrder(ctx, id)
}

func (s *Service) UpdatePurchaseOrder(ctx context.Context, id uint, in PurchaseOrder) (*PurchaseOrder, error) {
	return s.repo.UpdatePurchaseOrder(ctx, id, in)
}

func (s *Service) CreateGoodsReceipt(ctx context.Context, id uint, in GoodsReceipt) (*GoodsReceipt, error) {
	return s.repo.CreateGoodsReceipt(ctx, id, in)
}
