package identity

// CreateAccountInput is the request body for `POST /internal/accounts`.
// Not part of the ERD - this is an API-only shape for provisioning a brand
// new tenant in one call (Organization + owner User + a default Branch).
type CreateAccountInput struct {
	OrganizationName string `json:"organization_name" binding:"required"`
	OwnerEmail       string `json:"owner_email" binding:"required,email"`
	OwnerPassword    string `json:"owner_password" binding:"required,min=8"`
	BranchName       string `json:"branch_name" binding:"required"`
}

// CreateAccountResult is returned after provisioning a new tenant.
type CreateAccountResult struct {
	Organization Organization `json:"organization"`
	Owner        User         `json:"owner"`
	Branch       Branch       `json:"branch"`
}
