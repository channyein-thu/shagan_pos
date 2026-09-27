package audit

import "context"

// Interface defines the audit domain's use cases. Writing an entry isn't
// here - that happens directly through Repository (as AuditWriter) from
// inside whichever domain's own service method is performing the audited
// action, not through this Service - see e.g. sales.AuditWriter.
type Interface interface {
	// ListAuditLog backs `GET /audit-log` - see Repository's own doc for
	// scoping.
	ListAuditLog(ctx context.Context, orgID uint, branchID *uint) ([]AuditLog, error)
}
