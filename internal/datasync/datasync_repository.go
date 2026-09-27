package datasync

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Repository defines the datasync domain's persistence operations.
// SyncConflict carries no OrgID/BranchID of its own (only a SaleID), so
// every branch-scoped query here joins to sales - same
// cross-domain-via-table-name approach as shift.Expense's own
// branch-ownership check.
type Repository interface {
	// UnresolvedConflictCount backs GetSyncStatus's headline number,
	// scoped to branchIDs (already resolved by the service to the
	// caller's own org, or one verified branch).
	UnresolvedConflictCount(ctx context.Context, branchIDs []uint) (int64, error)
	// LastSyncedAt backs GetSyncStatus - the most recent Sale.SyncedAt
	// among sales in branchIDs, nil if none have ever synced. Only
	// IngestQueuedSales ever sets SyncedAt (see sales.Service.CreateSale's
	// doc) - a live-rung-up sale was never "synced" from anywhere.
	LastSyncedAt(ctx context.Context, branchIDs []uint) (*time.Time, error)
	// ListSyncConflicts is scoped to branchIDs, same reasoning as
	// UnresolvedConflictCount. Ordered newest-first.
	ListSyncConflicts(ctx context.Context, branchIDs []uint) ([]SyncConflict, error)
	// GetSyncConflictWithLock backs ResolveSyncConflict's existence check -
	// returns common.NotFoundError if id doesn't exist or its sale isn't in
	// branchIDs (not-found-not-forbidden, same reasoning as everywhere
	// else). Locks the row so two concurrent resolve attempts serialize
	// instead of racing.
	GetSyncConflictWithLock(db *gorm.DB, branchIDs []uint, id uint) (*SyncConflict, error)
	// UpdateSyncConflictResolved backs ResolveSyncConflict - plain field
	// write, db is the same in-flight transaction GetSyncConflictWithLock
	// locked the row in.
	UpdateSyncConflictResolved(db *gorm.DB, id uint, resolvedBy uint, resolvedAt time.Time) error
	// CreateSyncConflict backs IngestQueuedSales - a plain insert for each
	// NegativeStockEvent CreateSale reports back. Not run inside CreateSale's
	// own transaction (that's sealed inside the sales package, a different
	// domain) - see Service.IngestQueuedSales's doc for why that's an
	// acceptable, rare edge case here.
	CreateSyncConflict(ctx context.Context, c *SyncConflict) error
}
