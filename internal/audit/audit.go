package audit

import "context"

// Interface defines the audit domain's use cases.
type Interface interface {
	ListAuditLog(ctx context.Context) ([]AuditLog, error)
}

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListAuditLog(ctx context.Context) ([]AuditLog, error) {
	return s.repo.ListAuditLog(ctx)
}
