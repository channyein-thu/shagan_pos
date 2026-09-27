package datasync

import (
	"time"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/sales"
)

// CatalogSnapshot backs `GET /sync/catalog` - everything a device needs
// cached locally to ring up sales offline: current products (with their
// signed image URLs, same shape ListProducts already returns), categories,
// and combos. Paired with an ETag (see Service.GetCatalogSnapshot) so a
// device that already has the latest snapshot skips re-downloading it.
type CatalogSnapshot struct {
	Products   []catalog.ProductResult `json:"products"`
	Categories []catalog.Category      `json:"categories"`
	Combos     []catalog.ComboResult   `json:"combos"`
}

// IngestQueuedSalesRequest is the request body for `POST /sync/sales` - a
// batch of sales a device queued while offline, each shaped exactly like a
// normal `POST /sales` body (same client-generated ID, snapshotted prices -
// see sales.CreateSaleRequest's own doc for why that's already
// offline-safe).
type IngestQueuedSalesRequest struct {
	Sales []sales.CreateSaleRequest `json:"sales" binding:"required,min=1,dive"`
}

// IngestSaleResult is one queued sale's outcome within
// IngestQueuedSalesResponse - Error is set (and Success false) if this
// specific sale failed for a reason other than a stock conflict, which
// never fails the item (see Service.IngestQueuedSales's doc).
type IngestSaleResult struct {
	SaleID  string `json:"sale_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// IngestQueuedSalesResponse backs `POST /sync/sales` - per-item results, so
// the device can drop synced items from its local queue and retry only the
// failures (see docs/WORKFLOWS.md Section 10).
type IngestQueuedSalesResponse struct {
	Results []IngestSaleResult `json:"results"`
}

// SyncStatus backs `GET /sync/status`.
type SyncStatus struct {
	UnresolvedConflictCount int64      `json:"unresolved_conflict_count"`
	LastSyncedAt            *time.Time `json:"last_synced_at"`
}
