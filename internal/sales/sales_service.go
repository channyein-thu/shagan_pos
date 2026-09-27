package sales

import (
	"context"

	"github.com/google/uuid"
)

// SaleActor identifies the authenticated staff member ringing up a sale
// (derived from the verified X-Staff-Token, never client input), and
// whether their role's granted permissions let them apply a manual
// discount at all - checked against apply_manual_discount, same permission
// shift.ExpenseActor checks access_backoffice against.
type SaleActor struct {
	StaffID                uint
	CanApplyManualDiscount bool
}

// Interface defines the sales domain's use cases.
type Interface interface {
	// CreateSale derives Subtotal/Discount/Tax/Total from in.Items and
	// validates in.Payments sums to that Total - see Service.CreateSale.
	// branchID comes from the caller's pos-device access token, never
	// client input. Rejects (403) if any item carries a discount and actor
	// lacks CanApplyManualDiscount.
	CreateSale(ctx context.Context, orgID uint, branchID uint, actor SaleActor, in CreateSaleRequest) (*Sale, error)
	ListSales(ctx context.Context, orgID uint) ([]Sale, error)
	GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*Sale, error)
	// GetSaleReceipt and ReprintSale return the same bundle - a receipt's
	// content never changes between its first print and a reprint.
	GetSaleReceipt(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error)
	ReprintSale(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error)
	CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error)
	ListHeldSales(ctx context.Context) ([]HeldSale, error)
	ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error)
}
