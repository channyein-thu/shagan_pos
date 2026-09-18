package customer

import (
	"context"
	"errors"
	"time"

	"github.com/lib/pq"
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

// ListCustomers backs `GET /customers` when no search term is given.
func (r *RepositoryImpl) ListCustomers(ctx context.Context, orgID uint) ([]Customer, error) {
	var customers []Customer
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Find(&customers).Error; err != nil {
		return nil, err
	}
	return customers, nil
}

// SearchCustomers backs `GET /customers` when a search term is given - a
// case-insensitive partial match on name OR phone in one field, matching a
// POS lookup screen's single search box.
func (r *RepositoryImpl) SearchCustomers(ctx context.Context, orgID uint, search string) ([]Customer, error) {
	pattern := "%" + search + "%"
	var customers []Customer
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND (name ILIKE ? OR phone ILIKE ?)", orgID, pattern, pattern).
		Find(&customers).Error
	if err != nil {
		return nil, err
	}
	return customers, nil
}

// CreateCustomer backs `POST /customers`. Inline creation from the POS cart.
// ConsentStatus always starts at ConsentStatusPending - see
// CreateCustomerRequest's doc comment for why the client never sets this
// directly.
func (r *RepositoryImpl) CreateCustomer(ctx context.Context, orgID uint, in CreateCustomerRequest) (*Customer, error) {
	tags := in.Tags
	if tags == nil {
		tags = pq.StringArray{}
	}
	cust := Customer{
		OrgID:         orgID,
		Name:          in.Name,
		Phone:         in.Phone,
		Tags:          tags,
		ConsentStatus: ConsentStatusPending,
	}
	if err := r.db.WithContext(ctx).Create(&cust).Error; err != nil {
		if common.IsDuplicateError(err) {
			return nil, common.ConflictError("a customer with this phone number already exists")
		}
		return nil, err
	}
	return &cust, nil
}

// GetCustomer backs `GET /customers/:id`. Detail only for now - purchase
// history needs Sales, which doesn't exist yet (see the delivery plan).
func (r *RepositoryImpl) GetCustomer(ctx context.Context, orgID uint, id uint) (*Customer, error) {
	var cust Customer
	err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&cust).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("customer not found")
		}
		return nil, err
	}
	return &cust, nil
}

// ListCustomerConsents backs `GET /customers/:id/consents`.
func (r *RepositoryImpl) ListCustomerConsents(ctx context.Context, customerID uint) ([]CustomerConsent, error) {
	var consents []CustomerConsent
	if err := r.db.WithContext(ctx).Where("customer_id = ?", customerID).Find(&consents).Error; err != nil {
		return nil, err
	}
	return consents, nil
}

// CreateCustomerConsent backs `POST /customers/:id/consents` - just the
// insert. ChangedAt is a server-tracked audit timestamp, same treatment as
// Device.LastSeenAt. See Service.CreateCustomerConsent for how this is
// composed with UpdateCustomerConsentStatus inside one transaction.
func (r *RepositoryImpl) CreateCustomerConsent(db *gorm.DB, customerID uint, in CreateCustomerConsentRequest) (*CustomerConsent, error) {
	consent := CustomerConsent{
		CustomerID: customerID,
		Status:     in.Status,
		Source:     in.Source,
		ChangedAt:  time.Now(),
	}
	if err := db.Create(&consent).Error; err != nil {
		return nil, err
	}
	return &consent, nil
}

// UpdateCustomerConsentStatus backs Service.CreateCustomerConsent's cache
// update - a plain field write, no business logic.
func (r *RepositoryImpl) UpdateCustomerConsentStatus(db *gorm.DB, customerID uint, status ConsentStatus) error {
	return db.Model(&Customer{}).Where("id = ?", customerID).Update("consent_status", status).Error
}
