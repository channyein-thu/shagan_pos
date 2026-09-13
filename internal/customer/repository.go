package customer

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// ListCustomers backs `GET /customers`.
func (r *Repository) ListCustomers(ctx context.Context) ([]Customer, error) {
	return nil, common.ErrNotImplemented
}

// CreateCustomer backs `POST /customers`. Inline creation from the POS cart
func (r *Repository) CreateCustomer(ctx context.Context, in Customer) (*Customer, error) {
	return nil, common.ErrNotImplemented
}

// GetCustomer backs `GET /customers/:id`. Detail + purchase history
func (r *Repository) GetCustomer(ctx context.Context, id uint) (*Customer, error) {
	return nil, common.ErrNotImplemented
}

// ListCustomerConsents backs `GET /customers/:id/consents`.
func (r *Repository) ListCustomerConsents(ctx context.Context, id uint) ([]CustomerConsent, error) {
	return nil, common.ErrNotImplemented
}

// CreateCustomerConsent backs `POST /customers/:id/consents`. Also updates the cached customers.consent_status
func (r *Repository) CreateCustomerConsent(ctx context.Context, id uint, in CustomerConsent) (*CustomerConsent, error) {
	return nil, common.ErrNotImplemented
}
