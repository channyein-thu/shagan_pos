package sales

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/inventory"
)

// InventoryWriter is what sales needs from inventory: decrementing a sold
// product's stock at the selling branch and recording the ledger entry,
// same reasoning as procurement.InventoryWriter. inventory.Repository
// already satisfies this signature - no adapter needed.
type InventoryWriter interface {
	GetStockLevel(db *gorm.DB, productID uint, branchID uint) (*inventory.StockLevel, error)
	CreateStockLevel(db *gorm.DB, level *inventory.StockLevel) error
	UpdateStockLevelQty(db *gorm.DB, id uint, qty int) error
	CreateInventoryLedgerEntry(db *gorm.DB, entry *inventory.InventoryLedger) error
}

// AuditWriter is what sales needs from audit: recording a manual discount
// application as a privileged-action audit entry - see Service.CreateSale's
// own doc. audit.Repository already satisfies this signature - no adapter
// needed.
type AuditWriter interface {
	CreateAuditLog(db *gorm.DB, entry *audit.AuditLog) error
}

// SaleActor identifies the authenticated staff member ringing up a sale
// (derived from the verified X-Staff-Token, never client input), and
// whether their role's granted permissions let them apply a manual
// discount at all - checked against apply_manual_discount, same permission
// shift.ExpenseActor checks access_backoffice against.
type SaleActor struct {
	StaffID                uint
	CanApplyManualDiscount bool
}

// NegativeStockEvent is one product's stock going negative as a direct
// result of CreateSale being called with allowNegativeStock - see the
// Interface doc. datasync.Service.IngestQueuedSales is the only caller that
// ever sets allowNegativeStock, and inspects the returned events to decide
// which products need a SyncConflict row for a Manager/Owner to resolve.
type NegativeStockEvent struct {
	ProductID    uint
	BranchID     uint
	ResultingQty int
}

// Interface defines the sales domain's use cases.
type Interface interface {
	// CreateSale derives Subtotal/Discount/Tax/Total from in.Items and
	// validates in.Payments sums to that Total - see Service.CreateSale.
	// branchID comes from the caller's pos-device access token, never
	// client input. Rejects (403) if any item carries a discount and actor
	// lacks CanApplyManualDiscount. Decrements each item's product's stock
	// at branchID in the same transaction, rejecting (409) if any product
	// doesn't have enough - see Service.CreateSale's own doc for why this
	// is the sale's own branch, not the product's origin branch - unless
	// allowNegativeStock is true (only datasync.Service.IngestQueuedSales
	// ever sets it, resyncing a batch of offline-queued sales - see
	// docs/WORKFLOWS.md Section 10's sync-time-conflict rule), in which
	// case a product going negative is allowed through and reported back
	// as a NegativeStockEvent instead of rejected, and Sale.SyncedAt is
	// stamped (nil on the live path - see Sale.SyncedAt's own doc). Writes
	// an audit entry
	// when any item carried a discount - a manual discount is a privileged
	// action worth an accountability trail regardless of whether it came
	// from the cashier's own permission or a manager's approval.
	CreateSale(ctx context.Context, orgID uint, branchID uint, actor SaleActor, in CreateSaleRequest, allowNegativeStock bool) (*Sale, []NegativeStockEvent, error)
	ListSales(ctx context.Context, orgID uint) ([]Sale, error)
	GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*Sale, error)
	// GetSaleReceipt and ReprintSale return the same bundle - a receipt's
	// content never changes between its first print and a reprint.
	GetSaleReceipt(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error)
	ReprintSale(ctx context.Context, orgID uint, id uuid.UUID) (map[string]any, error)
	CreateHeldSale(ctx context.Context, branchID uint, staffID uint, in CreateHeldSaleRequest) (*HeldSale, error)
	// ListHeldSales and ResumeHeldSale are branch-scoped, not staff-scoped -
	// see Repository.ListHeldSales.
	ListHeldSales(ctx context.Context, branchID uint) ([]HeldSale, error)
	ResumeHeldSale(ctx context.Context, branchID uint, id uint) (*HeldSale, error)
}
