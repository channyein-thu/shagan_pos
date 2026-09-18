package customer

import (
	"context"

	"gorm.io/gorm"
)

// Repository defines the customer domain's persistence operations. Every
// method is a single, self-contained operation - multi-step orchestration
// (e.g. verifying a customer exists before acting on it, or writing a
// consent row and its cached status together) belongs in Service, not here.
type Repository interface {
	// ListCustomers and SearchCustomers are separate methods, not one method
	// branching internally on whether a search term was given - Service
	// picks which one to call.
	ListCustomers(ctx context.Context, orgID uint) ([]Customer, error)
	SearchCustomers(ctx context.Context, orgID uint, search string) ([]Customer, error)
	// CreateCustomer scopes the new row to orgID (the authenticated caller's
	// own organization) and rejects a duplicate phone within that org - see
	// Customer's ux_customers_org_phone index.
	CreateCustomer(ctx context.Context, orgID uint, in CreateCustomerRequest) (*Customer, error)
	// GetCustomer returns common.NotFoundError if id doesn't belong to orgID -
	// same not-found-not-forbidden reasoning as identity's GetBranch/GetStaff.
	GetCustomer(ctx context.Context, orgID uint, id uint) (*Customer, error)
	// ListCustomerConsents is not itself org-scoped - Service.ListCustomerConsents
	// calls GetCustomer first to confirm customerID belongs to the caller's
	// org, then this just reads by customerID.
	ListCustomerConsents(ctx context.Context, customerID uint) ([]CustomerConsent, error)
	// CreateCustomerConsent and UpdateCustomerConsentStatus each take db
	// (either the repository's own connection or an in-flight transaction)
	// instead of ctx, so Service.CreateCustomerConsent can compose them
	// inside one db.Transaction call - the audit row and the cached status
	// must never go out of sync with each other.
	CreateCustomerConsent(db *gorm.DB, customerID uint, in CreateCustomerConsentRequest) (*CustomerConsent, error)
	UpdateCustomerConsentStatus(db *gorm.DB, customerID uint, status ConsentStatus) error
}
