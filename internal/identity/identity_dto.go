package identity

import (
	"time"
)

// CreateBranchRequest is the request body for the endpoint that creates or updates a Branch.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateBranchRequest struct {
	OrgID   uint         `json:"org_id" binding:"required"`
	Name    string       `json:"name" binding:"required"`
	Status  BranchStatus `json:"status" binding:"required"`
	Address string       `json:"address" binding:"required"`
	Phone   string       `json:"phone" binding:"required"`
}

// CreateStaffRequest is the request body for the endpoint that creates or updates a Staff.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type CreateStaffRequest struct {
	BranchID uint        `json:"branch_id" binding:"required"`
	Name     string      `json:"name" binding:"required"`
	Role     uint        `json:"role" binding:"required"`
	PinHash  string      `json:"pin_hash" binding:"required"` // TODO: accept a plaintext secret here and hash it server-side - never a client-supplied hash
	Phone    string      `json:"phone" binding:"required"`
	Status   StaffStatus `json:"status" binding:"required"`
}

// RegisterDeviceRequest is the request body for the endpoint that creates or updates a Device.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type RegisterDeviceRequest struct {
	BranchID   uint         `json:"branch_id" binding:"required"`
	Name       string       `json:"name" binding:"required"`
	Status     DeviceStatus `json:"status" binding:"required"`
	LastSeenAt time.Time    `json:"last_seen_at" binding:"required"`
}

// UpdateBranchRequest is the request body for the endpoint that creates or updates a Branch.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateBranchRequest struct {
	OrgID   *uint         `json:"org_id" binding:"omitempty"`
	Name    *string       `json:"name" binding:"omitempty"`
	Status  *BranchStatus `json:"status" binding:"omitempty"`
	Address *string       `json:"address" binding:"omitempty"`
	Phone   *string       `json:"phone" binding:"omitempty"`
}

// UpdateDeviceRequest is the request body for the endpoint that creates or updates a Device.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateDeviceRequest struct {
	BranchID   *uint         `json:"branch_id" binding:"omitempty"`
	Name       *string       `json:"name" binding:"omitempty"`
	Status     *DeviceStatus `json:"status" binding:"omitempty"`
	LastSeenAt *time.Time    `json:"last_seen_at" binding:"omitempty"`
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

// UpdateStaffRequest is the request body for the endpoint that creates or updates a Staff.
// TODO: fields mirroring org/branch/staff/device ownership (e.g. OrgID, BranchID, StaffID)
// likely belong to the authenticated session/context, not client input - review before use.
type UpdateStaffRequest struct {
	BranchID *uint        `json:"branch_id" binding:"omitempty"`
	Name     *string      `json:"name" binding:"omitempty"`
	Role     *uint        `json:"role" binding:"omitempty"`
	PinHash  *string      `json:"pin_hash" binding:"omitempty"` // TODO: accept a plaintext secret here and hash it server-side - never a client-supplied hash
	Phone    *string      `json:"phone" binding:"omitempty"`
	Status   *StaffStatus `json:"status" binding:"omitempty"`
}

// --- Hand-written DTOs (not derived from the ERD/generator) ---

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
