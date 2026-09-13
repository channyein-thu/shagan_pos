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
