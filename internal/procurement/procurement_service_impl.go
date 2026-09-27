package procurement

import (
	"context"
	"time"

	"shagan_pos/internal/common"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListSuppliers(ctx context.Context, orgID uint) ([]Supplier, error) {
	return s.repo.ListSuppliers(ctx, orgID)
}

// CreateSupplier stamps LastOrderAt at creation time (no order has been
// placed yet, but the column is not-null) - it's updated to a real value
// later whenever an actual PurchaseOrder is placed, same reasoning as
// identity.CreateDevice's LastSeenAt.
func (s *Service) CreateSupplier(ctx context.Context, orgID uint, in CreateSupplierRequest) (*Supplier, error) {
	supplier := Supplier{
		OrgID:       orgID,
		Name:        in.Name,
		Phone:       in.Phone,
		Address:     in.Address,
		LastOrderAt: time.Now(),
	}
	if err := s.repo.CreateSupplier(ctx, &supplier); err != nil {
		return nil, err
	}
	return &supplier, nil
}

func (s *Service) UpdateSupplier(ctx context.Context, orgID uint, id uint, in UpdateSupplierRequest) (*Supplier, error) {
	if _, err := s.repo.GetSupplier(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Phone != nil {
		updates["phone"] = *in.Phone
	}
	if in.Address != nil {
		updates["address"] = *in.Address
	}

	if len(updates) > 0 {
		if err := s.repo.UpdateSupplier(ctx, id, updates); err != nil {
			return nil, err
		}
	}

	return s.repo.GetSupplier(ctx, orgID, id)
}

func (s *Service) DeleteSupplier(ctx context.Context, orgID uint, id uint) error {
	if _, err := s.repo.GetSupplier(ctx, orgID, id); err != nil {
		return err
	}

	inUse, err := s.repo.PurchaseOrdersExistForSupplier(ctx, id)
	if err != nil {
		return err
	}
	if inUse {
		return common.ConflictError("supplier is still in use by one or more purchase orders")
	}

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
