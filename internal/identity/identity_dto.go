package identity

import (
	"time"
)

// CreateBranchRequest is the request body for the endpoint that creates a
// Branch. OrgID is deliberately not here - the branch always belongs to the
// authenticated caller's own organization (see middleware.OrgIDFromContext),
// never a client-supplied org.
type CreateBranchRequest struct {
	Name    string       `json:"name" binding:"required"`
	Status  BranchStatus `json:"status" binding:"required"`
	Address string       `json:"address" binding:"required"`
	Phone   string       `json:"phone" binding:"required"`
}

// CreateStaffRequest is the request body for the endpoint that creates a
// Staff. BranchID is a legitimate client choice (an org can have several
// branches, so the caller picks which one) - the repository verifies it
// actually belongs to the caller's own org before using it. Pin is the
// plaintext 6-digit PIN; Service.CreateStaff hashes it before it ever
// reaches the repository - never accept a pre-hashed PIN from a client.
type CreateStaffRequest struct {
	BranchID uint        `json:"branch_id" binding:"required"`
	Name     string      `json:"name" binding:"required"`
	Role     uint        `json:"role" binding:"required"`
	Pin      string      `json:"pin" binding:"required,len=6,number"`
	Phone    string      `json:"phone" binding:"required"`
	Status   StaffStatus `json:"status" binding:"required"`
}

// CreateDeviceRequest is the request body for the endpoint that creates
// a Device. BranchID is a legitimate client choice (verified server-side
// against the caller's org). LastSeenAt is not here - it's a server-tracked
// heartbeat timestamp, set to now() on creation, never client input.
type CreateDeviceRequest struct {
	BranchID uint         `json:"branch_id" binding:"required"`
	Name     string       `json:"name" binding:"required"`
	Status   DeviceStatus `json:"status" binding:"required"`
}

// UpdateBranchRequest is the request body for the endpoint that updates a
// Branch. OrgID is deliberately not here - a branch can never be reassigned
// to a different organization via a client update.
type UpdateBranchRequest struct {
	Name    *string       `json:"name" binding:"omitempty"`
	Status  *BranchStatus `json:"status" binding:"omitempty"`
	Address *string       `json:"address" binding:"omitempty"`
	Phone   *string       `json:"phone" binding:"omitempty"`
}

// UpdateDeviceRequest is the request body for the endpoint that updates a
// Device (moving it to a different branch, renaming it, changing status).
// LastSeenAt still isn't client-writable - see CreateDeviceRequest.
type UpdateDeviceRequest struct {
	BranchID *uint         `json:"branch_id" binding:"omitempty"`
	Name     *string       `json:"name" binding:"omitempty"`
	Status   *DeviceStatus `json:"status" binding:"omitempty"`
}

// UpdateMeRequest is the request body for the endpoint that creates or updates a User.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateMeRequest struct {
	OrgID          *uint        `json:"org_id" binding:"omitempty"`
	Name           *string      `json:"name" binding:"omitempty"`
	AccountType    *AccountType `json:"account_type" binding:"omitempty"`
	DeviceID       *uint        `json:"device_id" binding:"omitempty"`
	Email          *string      `json:"email" binding:"omitempty"`
	CredentialHash *string      `json:"credential_hash" binding:"omitempty"` // TODO: accept a plaintext secret here and hash it server-side - never a client-supplied hash
}

// UpdateStaffRequest is the request body for the endpoint that updates a
// Staff. Same reasoning as CreateStaffRequest: BranchID (moving a staff
// member to a different branch) is re-verified against the caller's org, and
// Pin is a plaintext 6-digit PIN, hashed by Service.UpdateStaff before it reaches the repository.
type UpdateStaffRequest struct {
	BranchID *uint        `json:"branch_id" binding:"omitempty"`
	Name     *string      `json:"name" binding:"omitempty"`
	Role     *uint        `json:"role" binding:"omitempty"`
	Pin      *string      `json:"pin" binding:"omitempty,len=6,number"`
	Phone    *string      `json:"phone" binding:"omitempty"`
	Status   *StaffStatus `json:"status" binding:"omitempty"`
}

// --- Hand-written DTOs (not derived from the ERD/generator) ---

// CreateAccountInput is the request body for `POST /internal/accounts`.
// Not part of the ERD - this is an API-only shape for provisioning a brand
// new tenant: an Organization plus its owner User and service_center User
// (two distinct logins, each with their own email/password). Branch and
// Device creation happen afterward through the normal, already-authenticated
// POST /branches and POST /devices endpoints, using the org_id
// returned here - this endpoint no longer creates a Branch itself.
type CreateAccountInput struct {
	OrganizationName      string `json:"organization_name" binding:"required"`
	OwnerEmail            string `json:"owner_email" binding:"required,email"`
	OwnerPassword         string `json:"owner_password" binding:"required,min=8"`
	ServiceCenterEmail    string `json:"service_center_email" binding:"required,email"`
	ServiceCenterPassword string `json:"service_center_password" binding:"required,min=8"`
}

// CreateAccountResult is returned after provisioning a new tenant.
type CreateAccountResult struct {
	Organization  Organization `json:"organization"`
	Owner         User         `json:"owner"`
	ServiceCenter User         `json:"service_center"`
}

// CreateBranchInternalRequest is the request body for
// `POST /internal/branches`. Shagan-team-only: identical to
// CreateBranchRequest except OrgID is explicit, since there's no
// authenticated owner session to read it from (see
// middleware.OrgIDFromContext) - this is how Shagan's own tooling creates
// the first branch for a tenant just provisioned via CreateAccount.
type CreateBranchInternalRequest struct {
	OrgID   uint         `json:"org_id" binding:"required"`
	Name    string       `json:"name" binding:"required"`
	Status  BranchStatus `json:"status" binding:"required"`
	Address string       `json:"address" binding:"required"`
	Phone   string       `json:"phone" binding:"required"`
}

// CreateDeviceInternalRequest is the request body for
// `POST /internal/devices`. Same reasoning as CreateBranchInternalRequest -
// explicit OrgID since this is Shagan-team tooling, not an authenticated
// owner session.
type CreateDeviceInternalRequest struct {
	OrgID    uint         `json:"org_id" binding:"required"`
	BranchID uint         `json:"branch_id" binding:"required"`
	Name     string       `json:"name" binding:"required"`
	Status   DeviceStatus `json:"status" binding:"required"`
}

// CreatePosAccountInput is the request body for `POST /internal/accounts/pos`.
// Unlike CreateAccountInput, this attaches one User to an EXISTING
// Organization and Device - it never creates a new org. DeviceID implies
// which branch this account is tied to (via Device.BranchID), so BranchID
// isn't part of this request. There's no Name - a pos account represents a
// terminal/device credential, not a named person.
type CreatePosAccountInput struct {
	OrgID    uint   `json:"org_id" binding:"required"`
	DeviceID uint   `json:"device_id" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
}

// UpdateOrganizationStatusRequest is the request body for
// `PATCH /internal/organizations/:id/status`. Shagan-team-only lever for
// suspending/reactivating a whole tenant.
type UpdateOrganizationStatusRequest struct {
	Status OrganizationStatus `json:"status" binding:"required,oneof=active suspended"`
}

// UpdatePosAccountStatusRequest is the request body for
// `PATCH /internal/accounts/pos/:id/status`. Only ever applies to a
// pos-type User - see RepositoryImpl.UpdatePosAccountStatus.
type UpdatePosAccountStatusRequest struct {
	Status UserStatus `json:"status" binding:"required,oneof=active suspended"`
}

// ResetPosAccountPasswordRequest is the request body for
// `POST /internal/accounts/pos/:id/reset-password`. Password is plaintext -
// Service.ResetPosAccountPassword hashes it before it ever reaches the
// repository, same as every other password field in this package.
type ResetPosAccountPasswordRequest struct {
	Password string `json:"password" binding:"required,min=8"`
}

// LoginRequest is the request body for `POST /auth/login`.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// RefreshRequest is the request body for `POST /auth/refresh`.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// LogoutRequest is the request body for `POST /auth/logout`.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// SessionResult is returned on a successful login or refresh. RefreshToken is
// the plaintext token - it is shown to the client this one time only; the
// server stores just its hash (see Session.RefreshHash).
type SessionResult struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// VerifyStaffPINRequest is the request body for `POST /staff/:id/pin/verify`.
// Same PIN format as CreateStaffRequest/UpdateStaffRequest.
type VerifyStaffPINRequest struct {
	Pin string `json:"pin" binding:"required,len=6,number"`
}

// VerifyStaffPINResult is returned on a successful staff PIN verification.
// Token is a separate, shorter-lived JWT from the caller's own device/owner
// access token - see authtoken.StaffClaims.
type VerifyStaffPINResult struct {
	Staff     Staff     `json:"staff"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// VerifyManagerPINRequest is the request body for
// `POST /staff/:id/manager-pin/verify`. Permission is the specific
// permission code (e.g. "approve_void", "approve_return",
// "apply_manual_discount") that this approval is for - "manager" isn't one
// role check, since apply_manual_discount is also granted to super_staff
// while approve_void/approve_return are manager-only (see the v1
// role/permission grant table). This is for momentary, single-action
// elevation only - a manager wanting their own extended backoffice session
// should sign in via VerifyStaffPIN instead, not this endpoint.
type VerifyManagerPINRequest struct {
	Pin        string `json:"pin" binding:"required,len=6,number"`
	Permission string `json:"permission" binding:"required"`
}

// VerifyManagerPINResult is returned on a successful manager PIN
// verification. Same shape as VerifyStaffPINResult, but Token is
// deliberately much shorter-lived (see DefaultManagerPINTokenTTL) - it
// proves "a manager approved this one action just now", not "who's signed
// in for the shift".
type VerifyManagerPINResult struct {
	Staff     Staff     `json:"staff"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
