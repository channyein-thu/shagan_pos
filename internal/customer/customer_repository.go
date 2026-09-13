package customer

import "context"

// Repository defines the customer domain's persistence operations.
type Repository interface {
	ListCustomers(ctx context.Context) ([]Customer, error)
	CreateCustomer(ctx context.Context, in CreateCustomerRequest) (*Customer, error)
	GetCustomer(ctx context.Context, id uint) (*Customer, error)
	ListCustomerConsents(ctx context.Context, id uint) ([]CustomerConsent, error)
	CreateCustomerConsent(ctx context.Context, id uint, in CreateCustomerConsentRequest) (*CustomerConsent, error)
}
