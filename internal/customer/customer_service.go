package customer

import "context"

// Interface defines the customer domain's use cases.
type Interface interface {
	// ListCustomers picks ListCustomers or SearchCustomers on the repository
	// depending on whether search is empty - see Service.ListCustomers.
	ListCustomers(ctx context.Context, orgID uint, search string) ([]Customer, error)
	CreateCustomer(ctx context.Context, orgID uint, in CreateCustomerRequest) (*Customer, error)
	GetCustomer(ctx context.Context, orgID uint, id uint) (*Customer, error)
	// ListCustomerConsents confirms id belongs to orgID first (via
	// GetCustomer) before listing its consents.
	ListCustomerConsents(ctx context.Context, orgID uint, id uint) ([]CustomerConsent, error)
	// CreateCustomerConsent confirms id belongs to orgID first, then writes
	// the consent row and updates the cached consent_status as one atomic
	// unit - see Service.CreateCustomerConsent.
	CreateCustomerConsent(ctx context.Context, orgID uint, id uint, in CreateCustomerConsentRequest) (*CustomerConsent, error)
}
