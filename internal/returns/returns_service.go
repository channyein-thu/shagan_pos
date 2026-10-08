package returns

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/sales"
)

// BranchLookup is what returns needs from identity: resolving an owner's
// optional branch filter and enumerating an org's own branches, same
// reasoning as inventory.BranchLookup. identity.Repository already
// satisfies this signature - no adapter needed.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
	ListBranches(ctx context.Context, orgID uint) ([]identity.Branch, error)
}

// InventoryWriter is what returns needs from inventory: crediting a
// resellable returned/exchanged-back item's stock, decrementing a new
// exchanged-out item's stock, and recording the ledger entry - same
// reasoning as sales.InventoryWriter. inventory.Repository already
// satisfies this signature - no adapter needed.
type InventoryWriter interface {
	GetStockLevel(db *gorm.DB, productID uint, branchID uint) (*inventory.StockLevel, error)
	CreateStockLevel(db *gorm.DB, level *inventory.StockLevel) error
	UpdateStockLevelQty(db *gorm.DB, id uint, qty int) error
	CreateInventoryLedgerEntry(db *gorm.DB, entry *inventory.InventoryLedger) error
}

// AuditWriter is what returns needs from audit: recording each
// Void/Return/Exchange as a privileged-action audit entry, same reasoning
// as sales.InventoryWriter. audit.Repository already satisfies this
// signature - no adapter needed.
type AuditWriter interface {
	CreateAuditLog(db *gorm.DB, entry *audit.AuditLog) error
}

// SalesReader is what returns needs from sales: reading a sale (locked, so
// two concurrent Void/Return/Exchange attempts against the same sale
// serialize instead of racing) and its items, and marking a sale voided.
// sales.Repository already satisfies this signature - no adapter needed.
type SalesReader interface {
	GetSaleWithLock(db *gorm.DB, orgID uint, id uuid.UUID) (*sales.Sale, error)
	ListSaleItemsTx(db *gorm.DB, saleID uuid.UUID) ([]sales.SaleItem, error)
	UpdateSaleStatus(db *gorm.DB, id uuid.UUID, status sales.SaleStatus) error
	RequireOpenShift(db *gorm.DB, orgID uint, branchID uint, shiftID uint) error
}

// Actor identifies the authenticated staff member requesting a Void/Return/
// Exchange (derived from the verified X-Staff-Token, never client input),
// and whether they're allowed to approve this specific kind of action -
// true either because their own role grants the matching permission
// (approve_void/approve_return/approve_exchange) or because a manager
// approved this one action via X-Manager-Approval-Token, same
// StaffHasPermission-or-ManagerApproved shape as sales.SaleActor.
//
// UserID is the other shape of caller: an org-wide Owner / Service Center
// account acting directly with no PIN (the Owner has no Staff record) -
// StaffID is 0 then, and CanApprove is true (the Owner holds every approval
// permission implicitly). Only VoidSale accepts it today; exactly one of
// StaffID/UserID is non-zero.
type Actor struct {
	StaffID    uint
	UserID     uint
	CanApprove bool
}

// staffIDPtr/userIDPtr turn the zero value of whichever ID isn't in play
// into nil, for nullable columns (Void.ApprovedBy / ApprovedByUserID, the
// audit entry's actor, the ledger's actor).
func (a Actor) staffIDPtr() *uint {
	if a.StaffID == 0 {
		return nil
	}
	return &a.StaffID
}

func (a Actor) userIDPtr() *uint {
	if a.UserID == 0 {
		return nil
	}
	return &a.UserID
}

// Interface defines the returns domain's use cases. Every write method
// confirms the referenced Sale exists AND belongs to orgID first
// (not-found-not-forbidden, same reasoning as everywhere else) and rejects
// (403) unless actor.CanApprove.
type Interface interface {
	// VoidSale reverses the entire sale - see docs/WORKFLOWS.md Section 9.
	// Rejects (409) if the sale is already voided, if its shift isn't still
	// open (a void is only valid within the same shift it was rung up in),
	// or if any Return/Exchange already references it (Void credits back
	// each item's full original qty - allowing it after a partial Return/
	// Exchange already credited part of that qty back would double-credit
	// stock). Credits back every item's decremented stock and marks the
	// sale voided, atomically.
	VoidSale(ctx context.Context, orgID uint, actor Actor, saleID uuid.UUID, in VoidSaleRequest) (*Void, error)
	ListVoids(ctx context.Context, orgID uint, branchID *uint) ([]Void, error)
	// CreateReturn confirms the sale is completed (not voided - a voided
	// sale never happened, nothing to return) and every item belongs to it
	// with enough not-yet-returned qty remaining, computes RefundTotal
	// server-side, and restocks each Condition
	// ItemConditionSellable item at the sale's own branch - all atomically.
	CreateReturn(ctx context.Context, orgID uint, actor Actor, in CreateReturnRequest) (*Return, error)
	ListReturns(ctx context.Context, orgID uint, branchID *uint) ([]Return, error)
	GetReturn(ctx context.Context, orgID uint, id uint) (*Return, error)
	// CreateExchange confirms the sale is completed, computes NetDifference
	// server-side, credits stock for every "in" item and decrements it for
	// every "out" item at the sale's own branch - all atomically. Modeled
	// as one combined transaction record (Exchange/ExchangeItem), not a
	// chained Return-then-Sale.
	CreateExchange(ctx context.Context, orgID uint, actor Actor, in CreateExchangeRequest) (*Exchange, error)
	ListExchanges(ctx context.Context, orgID uint, branchID *uint) ([]Exchange, error)
	GetExchange(ctx context.Context, orgID uint, id uint) (*Exchange, error)
}
