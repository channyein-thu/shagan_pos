package datasync

import (
	"context"

	"github.com/google/uuid"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/sales"
)

// BranchLookup is what datasync needs from identity: resolving an owner's
// optional branch filter and enumerating an org's own branches, same
// reasoning as inventory.BranchLookup. identity.Repository already
// satisfies this signature - no adapter needed.
type BranchLookup interface {
	GetBranch(ctx context.Context, orgID uint, id uint) (*identity.Branch, error)
	ListBranches(ctx context.Context, orgID uint) ([]identity.Branch, error)
}

// CatalogReader is what datasync needs from catalog: the three lists that
// make up a device's offline cache - see CatalogSnapshot. catalog.Interface
// already satisfies this signature - no adapter needed. Deliberately the
// full Service (not a narrower Repository-level read), since a device
// needs the same client-ready shape (signed image URLs, combo items) the
// live GET /products and GET /combos already produce - reimplementing that
// composition here would just duplicate catalog's own business logic.
// Products/Categories/Combos are all org-wide, so every device in an org
// gets the exact same snapshot regardless of which branch it belongs to.
type CatalogReader interface {
	ListProducts(ctx context.Context, orgID uint) ([]catalog.ProductResult, error)
	ListCategories(ctx context.Context, orgID uint) ([]catalog.Category, error)
	ListCombos(ctx context.Context, orgID uint) ([]catalog.ComboResult, error)
}

// SalesWriter is what datasync needs from sales: checking whether a queued
// sale already arrived (idempotent retry) and creating it when it hasn't -
// see Service.IngestQueuedSales. sales.Interface already satisfies this
// signature - no adapter needed.
type SalesWriter interface {
	GetSale(ctx context.Context, orgID uint, id uuid.UUID) (*sales.Sale, error)
	CreateSale(ctx context.Context, orgID uint, branchID uint, actor sales.SaleActor, in sales.CreateSaleRequest, allowNegativeStock bool) (*sales.Sale, []sales.NegativeStockEvent, error)
}

// Interface defines the datasync domain's use cases.
type Interface interface {
	// GetCatalogSnapshot returns everything a device needs cached to sell
	// offline, plus an ETag over that content - if the caller's
	// If-None-Match header (compared by the handler, not here) matches, the
	// device already has the latest snapshot and gets a 304 instead of the
	// full body. Org-wide, same reasoning as CatalogReader's doc - no
	// branch filter.
	GetCatalogSnapshot(ctx context.Context, orgID uint) (*CatalogSnapshot, string, error)
	// IngestQueuedSales processes a device's offline queue - each sale is
	// idempotent by its own client-generated ID (a retry of an
	// already-arrived sale is a safe no-op, reported as success), and is
	// let through even if it would take a product's stock negative (that's
	// the one real cross-device conflict this domain expects - see
	// docs/WORKFLOWS.md Section 10), recording a SyncConflict instead of
	// rejecting so a Manager/Owner can resolve it later (e.g. via the
	// Inventory Ledger). Every queued sale is treated as pre-authorized
	// (manual discounts included) - it already happened at the register
	// under whatever authorization the device enforced at the time; this
	// backend's job during resync is to persist it, not re-litigate
	// permissions after the fact. A sale whose shift has since closed, or
	// that otherwise fails validation, is reported as a failed item (not a
	// SyncConflict) for the device to retry or flag - not silently
	// dropped.
	IngestQueuedSales(ctx context.Context, orgID uint, branchID uint, in IngestQueuedSalesRequest) (*IngestQueuedSalesResponse, error)
	GetSyncStatus(ctx context.Context, orgID uint, branchID *uint) (*SyncStatus, error)
	ListSyncConflicts(ctx context.Context, orgID uint, branchID *uint) ([]SyncConflict, error)
	// ResolveSyncConflict marks a conflict as reviewed - actorUserID is the
	// authenticated caller's own user ID, never client input. Rejects (409)
	// if it's already resolved. This only records that a human has seen and
	// dealt with it (e.g. by adjusting stock via the Inventory Ledger) - it
	// doesn't itself change any stock.
	ResolveSyncConflict(ctx context.Context, orgID uint, actorUserID uint, id uint) (*SyncConflict, error)
}
