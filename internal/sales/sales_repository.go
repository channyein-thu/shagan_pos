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
	// ListHeldSales is branch-scoped, not staff-scoped - any staff at the
	// branch can see every held sale there, not just their own (so a
	// covering cashier can resume one parked by whoever they're covering
	// for).
	ListHeldSales(ctx context.Context, branchID uint) ([]HeldSale, error)
	// ResumeHeldSale is an atomic delete-and-restore: returns
	// common.NotFoundError if id doesn't belong to branchID (same
	// not-found-not-forbidden reasoning as everywhere else), otherwise
	// deletes the row and returns what it held - resuming is inherently
	// one-time, there's nothing left to resume a second time.
	ResumeHeldSale(ctx context.Context, branchID uint, id uint) (*HeldSale, error)
	// GetSaleWithLock is GetSale's transaction-participating counterpart -
	// used by returns.Service to read-then-write a Sale atomically (voiding
	// it, or serializing concurrent Returns/Exchanges against the same
	// sale), same row-lock reasoning as inventory.Repository.GetStockTransfer.
	// Same not-found-not-forbidden reasoning as GetSale.
	GetSaleWithLock(db *gorm.DB, orgID uint, id uuid.UUID) (*Sale, error)
	// ListSaleItemsTx is ListSaleItems's transaction-participating
	// counterpart, same reasoning as GetSaleWithLock.
	ListSaleItemsTx(db *gorm.DB, saleID uuid.UUID) ([]SaleItem, error)
	// UpdateSaleStatus backs returns.Service.VoidSale - the only way a
	// Sale's Status ever changes after creation.
	UpdateSaleStatus(db *gorm.DB, id uuid.UUID, status SaleStatus) error
}
