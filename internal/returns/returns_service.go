package returns

import (
	"context"

	"github.com/google/uuid"
)

// Interface defines the returns domain's use cases.
type Interface interface {
	VoidSale(ctx context.Context, id uuid.UUID, in VoidSaleRequest) (*Void, error)
	ListVoids(ctx context.Context) ([]Void, error)
	CreateReturn(ctx context.Context, in CreateReturnRequest) (*Return, error)
	ListReturns(ctx context.Context) ([]Return, error)
	GetReturn(ctx context.Context, id uint) (*Return, error)
	CreateExchange(ctx context.Context, in CreateExchangeRequest) (*Exchange, error)
	ListExchanges(ctx context.Context) ([]Exchange, error)
	GetExchange(ctx context.Context, id uint) (*Exchange, error)
}
