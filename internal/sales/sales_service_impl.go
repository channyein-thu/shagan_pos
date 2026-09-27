package sales

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Service struct {
	repo Repository
	db   common.Transactioner
}

func NewService(repo Repository, db common.Transactioner) *Service {
	return &Service{repo: repo, db: db}
}

var _ Interface = (*Service)(nil)

// CreateSale derives Subtotal/Discount/Tax/Total from in.Items rather than
// trusting client-computed totals, rejects a payments total that doesn't
// cover the derived Total, then persists the Sale/SaleItems/Payments as one
// atomic unit guarded by a currently-open-shift check - same
// transaction-composes-atomic-primitives shape as identity.Service.CreateAccount.
//
// A discount on any item requires actor.CanApplyManualDiscount - a plain
// Staff member's own token won't have it (only super_staff/manager do, per
// the seeded role grants); it's true either because the calling staff holds
// apply_manual_discount themselves, or because a manager approved this sale
// via X-Manager-Approval-Token (see middleware.ManagerApproved) - both are
// folded into one bool by the handler before this is ever called, so this
// method doesn't need to know which case applied.
func (s *Service) CreateSale(ctx context.Context, orgID uint, branchID uint, actor SaleActor, in CreateSaleRequest) (*Sale, error) {
	saleItems := make([]SaleItem, 0, len(in.Items))
	subtotal := decimal.Zero
	itemDiscountTotal := decimal.Zero
	itemTaxTotal := decimal.Zero
	for _, item := range in.Items {
		if item.Discount.IsNegative() {
			return nil, common.BadRequestError("item discount must be zero or greater")
		}
		if item.Tax.IsNegative() {
			return nil, common.BadRequestError("item tax must be zero or greater")
		}
		effectivePrice := item.UnitPrice
		if item.PriceOverride != nil {
			effectivePrice = *item.PriceOverride
		}
		lineGross := effectivePrice.Mul(decimal.NewFromInt(int64(item.Qty)))
		subtotal = subtotal.Add(lineGross)
		itemDiscountTotal = itemDiscountTotal.Add(item.Discount)
		itemTaxTotal = itemTaxTotal.Add(item.Tax)
		saleItems = append(saleItems, SaleItem{
			SaleID:        in.ID,
			ProductID:     item.ProductID,
			ComboID:       item.ComboID,
			NameSnapshot:  item.NameSnapshot,
			UnitPrice:     item.UnitPrice,
			PriceOverride: item.PriceOverride,
			Qty:           item.Qty,
			LineTotal:     lineGross.Sub(item.Discount),
			Discount:      item.Discount,
			Tax:           item.Tax,
		})
	}
	if itemDiscountTotal.IsPositive() && !actor.CanApplyManualDiscount {
		return nil, common.ForbiddenError("staff does not have permission to apply a manual discount")
	}
	total := subtotal.Sub(itemDiscountTotal).Add(itemTaxTotal)

	payments := make([]Payment, 0, len(in.Payments))
	paymentsTotal := decimal.Zero
	for _, p := range in.Payments {
		paymentsTotal = paymentsTotal.Add(p.Amount)
		payments = append(payments, Payment{
			SaleID:         in.ID,
			Method:         p.Method,
			Amount:         p.Amount,
			AmountReceived: p.AmountReceived,
			ChangeGiven:    p.ChangeGiven,
		})
	}
	if !paymentsTotal.Equal(total) {
		return nil, common.BadRequestError("payments must add up to the sale total")
	}

	now := time.Now()
	sale := &Sale{
		ID:          in.ID,
		OrgID:       orgID,
		BranchID:    branchID,
		ShiftID:     in.ShiftID,
		StaffID:     actor.StaffID,
		DeviceID:    in.DeviceID,
		CustomerID:  in.CustomerID,
		Subtotal:    subtotal,
		Discount:    itemDiscountTotal,
		Tax:         itemTaxTotal,
		Total:       total,
		Status:      SaleStatusCompleted,
		CompletedAt: &now,
	}

	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.repo.RequireOpenShift(tx, orgID, branchID, in.ShiftID); err != nil {
			return err
		}
		if err := s.repo.CreateSale(tx, sale); err != nil {
			return err
		}
		if err := s.repo.CreateSaleItems(tx, saleItems); err != nil {
			return err
		}
		return s.repo.CreatePayments(tx, payments)
	})
	if err != nil {
		return nil, err
	}
	return sale, nil
}

func (s *Service) ListSales(ctx context.Context, orgID uint) ([]Sale, error) {
	return s.repo.ListSales(ctx, orgID)
}

func (s *Service) GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*Sale, error) {
	return s.repo.GetSale(ctx, orgID, id)
}

func (s *Service) GetSaleReceipt(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error) {
	sale, err := s.repo.GetSale(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.ListSaleItems(ctx, id)
	if err != nil {
		return nil, err
	}
	payments, err := s.repo.ListPayments(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sale": sale, "items": items, "payments": payments}, nil
}

func (s *Service) ReprintSale(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error) {
	return s.GetSaleReceipt(ctx, orgID, id)
}

func (s *Service) CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error) {
	return s.repo.CreateHeldSale(ctx, in)
}

func (s *Service) ListHeldSales(ctx context.Context) ([]HeldSale, error) {
	return s.repo.ListHeldSales(ctx)
}

func (s *Service) ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error) {
	return s.repo.ResumeHeldSale(ctx, id)
}
