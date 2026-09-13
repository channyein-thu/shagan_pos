package audit

import "context"

// Repository defines the audit domain's persistence operations.
type Repository interface {
	ListAuditLog(ctx context.Context) ([]AuditLog, error)
}
