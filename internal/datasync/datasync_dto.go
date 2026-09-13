package datasync

// No request DTOs yet: every endpoint here is either a read (ListSyncConflicts),
// an internal action with no meaningful body (ResolveSyncConflict), or a
// cross-domain sync operation (GetCatalogSnapshot, IngestQueuedSales, FlushSync,
// GetSyncStatus) whose payload shape isn't defined yet.
