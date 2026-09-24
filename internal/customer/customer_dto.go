package customer

import (
	"github.com/lib/pq"
)

// CreateCustomerConsentRequest is the request body for
// `POST /customers/:id/consents`. CustomerID isn't here - it's the :id path
// param, the single source of truth for which customer this is about (same
// reasoning as OrgID never appearing in CreateCustomerRequest). ChangedAt
// isn't client-settable either - it's a server-tracked audit timestamp, set
// to now() when the row is created, same as Device.LastSeenAt.
type CreateCustomerConsentRequest struct {
	Status ConsentStatus `json:"status" binding:"required"`
	Source ConsentSource `json:"source" binding:"required"`
}

// CreateCustomerRequest is the request body for the endpoint that creates a
// Customer. OrgID is deliberately not here - a customer always belongs to
// the authenticated caller's own organization (see middleware.OrgIDFromContext),
// never a client-supplied org. ConsentStatus isn't here either - it's a
// cache of the customer's latest real CustomerConsent record (see
// CreateCustomerConsentRequest), and a customer created inline from the POS
// cart hasn't consented to anything yet, so the server always starts it at
// ConsentStatusPending; the only legitimate way to change it afterward is
// through POST /customers/:id/consents. Tags is optional - a quick inline
// creation shouldn't be blocked on supplying metadata tags.
type CreateCustomerRequest struct {
	Name  string         `json:"name" binding:"required"`
	Phone string         `json:"phone" binding:"required"`
	Tags  pq.StringArray `json:"tags" binding:"omitempty"`
}
