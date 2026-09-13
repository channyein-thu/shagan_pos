package customer

import (
	"time"

	"github.com/lib/pq"
)

// TODO: relationships (belongs-to/has-many) are intentionally omitted here;
// wire them up as needed in repository.go queries.
// ConsentStatus is a best-guess enum (ERD only specified "enum"; confirm real values).
type ConsentStatus string

const (
	ConsentStatusGranted ConsentStatus = "granted"
	ConsentStatusRevoked ConsentStatus = "revoked"
	ConsentStatusPending ConsentStatus = "pending"
)

// ConsentSource is a best-guess enum (ERD only specified "enum"; confirm real values).
type ConsentSource string

const (
	ConsentSourcePos    ConsentSource = "pos"
	ConsentSourceOnline ConsentSource = "online"
	ConsentSourceSms    ConsentSource = "sms"
	ConsentSourceEmail  ConsentSource = "email"
)

// CustomerConsent maps to the "Customer_consents" table in the ERD.
type CustomerConsent struct {
	ID         uint          `gorm:"primaryKey;autoIncrement" json:"id"`
	CustomerID uint          `gorm:"index;not null" json:"customer_id"`
	Status     ConsentStatus `gorm:"type:varchar(30);not null" json:"status"` // one of ConsentStatus* constants below (TODO: confirm real values)
	Source     ConsentSource `gorm:"type:varchar(30);not null" json:"source"` // one of ConsentSource* constants below (TODO: confirm real values)
	ChangedAt  time.Time     `gorm:"not null" json:"changed_at"`
}

// Customer maps to the "Customers" table in the ERD.
type Customer struct {
	ID            uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID         uint           `gorm:"not null;uniqueIndex:ux_customers_org_phone" json:"org_id"`
	Name          string         `gorm:"size:255;not null" json:"name"`
	Phone         string         `gorm:"size:25;uniqueIndex:ux_customers_org_phone;not null" json:"phone"`
	Tags          pq.StringArray `gorm:"type:text[];not null" json:"tags"`
	ConsentStatus ConsentStatus  `gorm:"type:varchar(30);not null" json:"consent_status"` // one of ConsentStatus* constants below (TODO: confirm real values)
	CreatedAt     time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
}
