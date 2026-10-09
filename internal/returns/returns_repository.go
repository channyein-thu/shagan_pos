package returns

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/sales"
)

// Repository defines the returns domain's persistence operations. Void/
// Return/Exchange carry no OrgID/BranchID of their own (only a SaleID), so
// every List/Get here is scoped by joining to sales - same
// cross-domain-via-table-name approach as shift.Expense's own
// branch-ownership check.
type Repository interface {
	// CreateVoid backs VoidSale. Plain insert.
	CreateVoid(db *gorm.DB, v *Void) error
	// ListVoids is scoped to branchIDs (already resolved by the service to
	// the caller's own org, or one verified branch) via a join to sales.
	ListVoids(ctx context.Context, branchIDs []uint) ([]Void, error)
	// CreateReturn and CreateReturnItems are the plain inserts
	// Service.CreateReturn composes inside one transaction - a return
	// without its line items should never exist, same reasoning as
	// procurement's PurchaseOrder/PurchaseOrderItems.
	CreateReturn(db *gorm.DB, r *Return) error
	CreateReturnItems(db *gorm.DB, items []ReturnItem) error
	ListReturns(ctx context.Context, branchIDs []uint) ([]Return, error)
	// GetReturn returns common.NotFoundError if id doesn't exist or its
	// sale isn't in branchIDs - not-found-not-forbidden, same reasoning as
	// everywhere else.
	GetReturn(ctx context.Context, branchIDs []uint, id uint) (*Return, error)
	// ReturnedQtyForSaleItem sums Qty across every prior ReturnItem
	// referencing saleItemID - guards CreateReturn against returning more
	// than was originally sold across multiple partial returns.
	ReturnedQtyForSaleItem(db *gorm.DB, saleItemID uint) (int, error)
	// ExchangedInQtyForSaleItem sums Qty across every prior "in"-direction
	// ExchangeItem referencing saleItemID - CreateExchange combines this
	// with ReturnedQtyForSaleItem so a Return and an Exchange against the
	// same sale item can't jointly exceed what was originally sold.
	ExchangedInQtyForSaleItem(db *gorm.DB, saleItemID uint) (int, error)
	// SaleHasReturnOrExchange reports whether any Return or Exchange
	// already references saleID - VoidSale rejects if so, since it credits
	// back a sale item's full original qty and would double-credit
	// whatever a prior partial Return/Exchange already credited.
	SaleHasReturnOrExchange(db *gorm.DB, saleID uuid.UUID) (bool, error)
	// CreateExchange and CreateExchangeItems are the plain inserts
	// Service.CreateExchange composes inside one transaction, same
	// reasoning as CreateReturn/CreateReturnItems.
	CreateExchange(db *gorm.DB, e *Exchange) error
	CreateExchangeItems(db *gorm.DB, items []ExchangeItem) error
	ListExchanges(ctx context.Context, branchIDs []uint) ([]Exchange, error)
	// GetExchange returns common.NotFoundError if id doesn't exist or its
	// sale isn't in branchIDs - same reasoning as GetReturn.
	GetExchange(ctx context.Context, branchIDs []uint, id uint) (*Exchange, error)
	// CurrentShiftID resolves the open shift of the till behind userID (a
	// paired POS account): its own device's most recent open shift, or nil
	// when userID isn't a paired POS account in orgID or its device has no
	// open shift. Nil is a normal answer, not an error - the caller decides
	// what a return/exchange with no shift means. Same lookup as
	// shift.Repository.GetCurrentShift, done here by table name rather than
	// by importing shift (shift's own close/summary logic reads this
	// domain's tables, so importing it back would be a cycle) - same
	// cross-domain-via-table-name approach as sales.RequireOpenShift.
	CurrentShiftID(db *gorm.DB, orgID uint, userID uint) (*uint, error)
	// ReturnedQtyBySaleItems and ReturnSummaries are what the sales receipt
	// and history read (sales.ReturnActivityReader). The first is the batch
	// form of ReturnedQtyForSaleItem + ExchangedInQtyForSaleItem: per
	// sale_item_id, qty on return lines plus qty on exchange "in" lines,
	// absent when nothing came back. The second is keyed by sale ID and
	// absent for a sale with no return or exchange.
	ReturnedQtyBySaleItems(ctx context.Context, saleItemIDs []uint) (map[uint]int, error)
	ReturnSummaries(ctx context.Context, saleIDs []uuid.UUID) (map[uuid.UUID]sales.ReturnSummary, error)
}
