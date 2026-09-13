package customer

import "context"

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

var _ Interface = (*Service)(nil)

func (s *Service) ListCustomers(ctx context.Context) ([]Customer, error) {
	return s.repo.ListCustomers(ctx)
}

func (s *Service) CreateCustomer(ctx context.Context, in CreateCustomerRequest) (*Customer, error) {
	return s.repo.CreateCustomer(ctx, in)
}

func (s *Service) GetCustomer(ctx context.Context, id uint) (*Customer, error) {
	return s.repo.GetCustomer(ctx, id)
}

func (s *Service) ListCustomerConsents(ctx context.Context, id uint) ([]CustomerConsent, error) {
	return s.repo.ListCustomerConsents(ctx, id)
}

func (s *Service) CreateCustomerConsent(ctx context.Context, id uint, in CreateCustomerConsentRequest) (*CustomerConsent, error) {
	return s.repo.CreateCustomerConsent(ctx, id, in)
}
