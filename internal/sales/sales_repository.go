package sales

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository defines the sales domain's persistence operations.
type Repository interface {
	// RequireOpenShift returns common.NotFoundError unless shiftID exists,
	// belongs to branchID, belongs (via branchID) to orgID, and is currently
	// open - a sale can never be rung up against a closed or foreign shift.
	// Takes db instead of ctx so it can run inside the same transaction as
	// the writes it's guarding - see Service.CreateSale.
	RequireOpenShift(db *gorm.DB, orgID uint, branchID uint, shiftID uint) error
	// CreateSale, CreateSaleItems and CreatePayments are the plain inserts
	// Service.CreateSale composes inside one db.Transaction call - each just
	// persists what it's given, same pattern as identity.CreateOrganization
	// and identity.CreateUser.
	CreateSale(db *gorm.DB, sale *Sale) error
	CreateSaleItems(db *gorm.DB, items []SaleItem) error
	CreatePayments(db *gorm.DB, payments []Payment) error
	ListSales(ctx context.Context, orgID uint) ([]Sale, error)
	// GetSale returns common.NotFoundError for a sale that exists but
	// belongs to a different org, same as one that doesn't exist at all -
	// same not-found-not-forbidden reasoning as everywhere else.
	GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*Sale, error)
	ListSaleItems(ctx context.Context, saleID uuid.UUID) ([]SaleItem, error)
	ListPayments(ctx context.Context, saleID uuid.UUID) ([]Payment, error)
	CreateHeldSale(ctx context.Context, in CreateHeldSaleRequest) (*HeldSale, error)
	ListHeldSales(ctx context.Context) ([]HeldSale, error)
	ResumeHeldSale(ctx context.Context, id uint) (*HeldSale, error)
}
