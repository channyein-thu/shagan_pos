package audit

import (
	"context"

	"gorm.io/gorm"
)

// Repository defines the audit domain's persistence operations.
type Repository interface {
	// CreateAuditLog backs every domain's own privileged-action logging
	// (see e.g. sales.AuditWriter) - a plain insert. db is either the
	// repository's normal connection or an in-flight transaction handed
	// down by the caller, so an audit entry commits or rolls back together
	// with whatever action it's recording, when the caller has one.
	CreateAuditLog(db *gorm.DB, entry *AuditLog) error
	// ListAuditLog backs `GET /audit-log`, scoped to orgID (AuditLog
	// carries its own OrgID directly, unlike Void/Return/Exchange - no
	// cross-domain join needed) and optionally narrowed to one branch.
	// Ordered newest-first.
	ListAuditLog(ctx context.Context, orgID uint, branchID *uint) ([]AuditLog, error)
}
