package sales

import (
	"context"

	"github.com/google/uuid"
)

// Interface defines the sales domain's use cases.
type Interface interface {
	CreateSale(ctx context.Context, in CreateSaleRequest) (*Sale, error)
	ListSales(ctx context.Context) ([]Sale, error)
	GetSale(ctx context.Context, id uuid.UUID) (*Sale, error)
	GetSaleReceipt(ctx context.Context, id uuid.UUID) (map[string]any, error)
	ReprintSale(ctx context.Context, id uuid.UUID) (map[string]any, error)
	CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error)
	ListHeldSales(ctx context.Context) ([]HeldSale, error)
	ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error)
}
