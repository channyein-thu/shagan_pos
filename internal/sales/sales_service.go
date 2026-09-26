package sales

import (
	"context"

	"github.com/google/uuid"
)

// Interface defines the sales domain's use cases.
type Interface interface {
	// CreateSale derives Subtotal/Discount/Tax/Total from in.Items and
	// validates in.Payments sums to that Total - see Service.CreateSale.
	// branchID comes from the caller's pos-device access token, never
	// client input.
	CreateSale(ctx context.Context, orgID uint, branchID uint, in CreateSaleRequest) (*Sale, error)
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
