package customer

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Service struct {
	repo Repository
	db   common.Transactioner
}

func NewService(repo Repository, db common.Transactioner) *Service {
	return &Service{repo: repo, db: db}
}

var _ Interface = (*Service)(nil)

// ListCustomers calls SearchCustomers when a search term is given, ListCustomers
// otherwise - the repository exposes two simple, separate methods rather
// than one method branching internally.
func (s *Service) ListCustomers(ctx context.Context, orgID uint, search string) ([]Customer, error) {
	if search == "" {
		return s.repo.ListCustomers(ctx, orgID)
	}
	return s.repo.SearchCustomers(ctx, orgID, search)
}

func (s *Service) CreateCustomer(ctx context.Context, orgID uint, in CreateCustomerRequest) (*Customer, error) {
	return s.repo.CreateCustomer(ctx, orgID, in)
}

func (s *Service) GetCustomer(ctx context.Context, orgID uint, id uint) (*Customer, error) {
	return s.repo.GetCustomer(ctx, orgID, id)
}

func (s *Service) ListCustomerConsents(ctx context.Context, orgID uint, id uint) ([]CustomerConsent, error) {
	if _, err := s.repo.GetCustomer(ctx, orgID, id); err != nil {
		return nil, err
	}
	return s.repo.ListCustomerConsents(ctx, id)
}

// CreateCustomerConsent writes the real audit row and updates the cached
// customers.consent_status as one atomic unit inside a transaction - a
// failure partway through must never leave the cache out of sync with the
// actual consent history (see Repository's doc comment on why these two
// methods take db instead of ctx).
func (s *Service) CreateCustomerConsent(ctx context.Context, orgID uint, id uint, in CreateCustomerConsentRequest) (*CustomerConsent, error) {
	if _, err := s.repo.GetCustomer(ctx, orgID, id); err != nil {
		return nil, err
	}

	var consent *CustomerConsent
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		consent, err = s.repo.CreateCustomerConsent(tx, id, in)
		if err != nil {
			return err
		}
		return s.repo.UpdateCustomerConsentStatus(tx, id, in.Status)
	})
	if err != nil {
		return nil, err
	}
	return consent, nil
}
