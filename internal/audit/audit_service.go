package audit

import "context"

// Interface defines the audit domain's use cases.
type Interface interface {
	ListAuditLog(ctx context.Context) ([]AuditLog, error)
}
