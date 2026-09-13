package customer

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// ListCustomers backs `GET /customers`.
func (r *RepositoryImpl) ListCustomers(ctx context.Context) ([]Customer, error) {
	return nil, common.ErrNotImplemented
}

// CreateCustomer backs `POST /customers`. Inline creation from the POS cart
func (r *RepositoryImpl) CreateCustomer(ctx context.Context, in CreateCustomerRequest) (*Customer, error) {
	return nil, common.ErrNotImplemented
}

// GetCustomer backs `GET /customers/:id`. Detail + purchase history
func (r *RepositoryImpl) GetCustomer(ctx context.Context, id uint) (*Customer, error) {
	return nil, common.ErrNotImplemented
}

// ListCustomerConsents backs `GET /customers/:id/consents`.
func (r *RepositoryImpl) ListCustomerConsents(ctx context.Context, id uint) ([]CustomerConsent, error) {
	return nil, common.ErrNotImplemented
}

// CreateCustomerConsent backs `POST /customers/:id/consents`. Also updates the cached customers.consent_status
func (r *RepositoryImpl) CreateCustomerConsent(ctx context.Context, id uint, in CreateCustomerConsentRequest) (*CustomerConsent, error) {
	return nil, common.ErrNotImplemented
}
