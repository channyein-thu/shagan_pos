package common

import (
	"database/sql"

	"gorm.io/gorm"
)

// Transactioner is the one *gorm.DB method a service needs to run a use case
// atomically across several repository writes. *gorm.DB already implements
// this natively, so production code needs no adapter - a service just holds
// its *gorm.DB as a Transactioner field. Depending on this narrow interface
// instead of the concrete *gorm.DB type is what keeps a use case like
// identity.Service.CreateAccount unit-testable with a two-line fake, while
// the repository methods it calls (see identity.Repository.CreateOrganization/
// CreateUser) still take db *gorm.DB directly, same as before.
type Transactioner interface {
	Transaction(fc func(tx *gorm.DB) error, opts ...*sql.TxOptions) error
}
