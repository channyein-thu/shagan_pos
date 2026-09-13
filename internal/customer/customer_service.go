package customer

import "context"

// Interface defines the customer domain's use cases.
type Interface interface {
	ListCustomers(ctx context.Context) ([]Customer, error)
	CreateCustomer(ctx context.Context, in CreateCustomerRequest) (*Customer, error)
	GetCustomer(ctx context.Context, id uint) (*Customer, error)
	ListCustomerConsents(ctx context.Context, id uint) ([]CustomerConsent, error)
	CreateCustomerConsent(ctx context.Context, id uint, in CreateCustomerConsentRequest) (*CustomerConsent, error)
}
