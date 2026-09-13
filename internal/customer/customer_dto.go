package customer

import (
	"time"

	"github.com/lib/pq"
)

// CreateCustomerConsentRequest is the request body for the endpoint that creates or updates a CustomerConsent.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateCustomerConsentRequest struct {
	CustomerID uint          `json:"customer_id" binding:"required"`
	Status     ConsentStatus `json:"status" binding:"required"`
	Source     ConsentSource `json:"source" binding:"required"`
	ChangedAt  time.Time     `json:"changed_at" binding:"required"`
}

// CreateCustomerRequest is the request body for the endpoint that creates or updates a Customer.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateCustomerRequest struct {
	OrgID         uint           `json:"org_id" binding:"required"`
	Name          string         `json:"name" binding:"required"`
	Phone         string         `json:"phone" binding:"required"`
	Tags          pq.StringArray `json:"tags" binding:"required"`
	ConsentStatus ConsentStatus  `json:"consent_status" binding:"required"`
}
