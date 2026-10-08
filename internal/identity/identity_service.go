package identity

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
)

// AuditWriter is what identity needs from audit: recording a privileged
// staff change as an audit entry, same reasoning as sales.AuditWriter.
// audit.Repository already satisfies this signature - no adapter needed.
type AuditWriter interface {
	CreateAuditLog(db *gorm.DB, entry *audit.AuditLog) error
}

// Interface defines the identity domain's use cases.
type Interface interface {
	Login(ctx context.Context, in LoginRequest) (*SessionResult, error)
	RefreshSession(ctx context.Context, in RefreshRequest) (*SessionResult, error)
	Logout(ctx context.Context, in LogoutRequest) error
	GetMe(ctx context.Context, userID uint) (*User, error)
	UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error)
	// VerifyManagerPIN backs `POST /staff/:id/manager-pin/verify`. Same
	// org/branch scoping and generic-401 reasoning as VerifyStaffPIN below,
	// plus a check that the target staff's role actually grants in.Permission
	// - momentary single-action elevation only, not a backoffice sign-in (use
	// VerifyStaffPIN for that).
	VerifyManagerPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyManagerPINRequest) (*VerifyManagerPINResult, error)
	// VerifyStaffPIN backs `POST /staff/:id/pin/verify`. branchID is the
	// caller's own branch (nil for an owner/service_center token, which isn't
	// branch-locked) - when set, the staff being verified must belong to that
	// same branch, same not-found-not-forbidden reasoning as GetStaff, folded
	// into the same generic 401 as a wrong PIN so a terminal can't enumerate
	// staff IDs by probing.
	VerifyStaffPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyStaffPINRequest) (*VerifyStaffPINResult, error)
	CreateDevice(ctx context.Context, orgID uint, in CreateDeviceRequest) (*Device, error)
	ListDevices(ctx context.Context, orgID uint) ([]Device, error)
	UpdateDevice(ctx context.Context, orgID uint, id uint, in UpdateDeviceRequest) (*Device, error)
	ListBranches(ctx context.Context, orgID uint) ([]Branch, error)
	CreateBranch(ctx context.Context, orgID uint, in CreateBranchRequest) (*Branch, error)
	GetBranch(ctx context.Context, orgID uint, id uint) (*Branch, error)
	UpdateBranch(ctx context.Context, orgID uint, id uint, in UpdateBranchRequest) (*Branch, error)
	ListBranchStaff(ctx context.Context, orgID uint, id uint) ([]Staff, error)
	ListBranchManagers(ctx context.Context, orgID uint, id uint, permissionCode string) ([]Staff, error)
	ListStaff(ctx context.Context, orgID uint, branchID *uint) ([]Staff, error)
	CreateStaff(ctx context.Context, orgID uint, in CreateStaffRequest) (*Staff, error)
	GetStaff(ctx context.Context, orgID uint, id uint) (*Staff, error)
	// UpdateStaff writes an audit entry (Entity "staff") for the change -
	// editing roles/PINs/status is Owner-only and privileged (see
	// docs/WORKFLOWS.md Section 4), worth an accountability trail.
	// actorUserID is the authenticated caller's own user ID, never client
	// input.
	UpdateStaff(ctx context.Context, orgID uint, actorUserID uint, id uint, in UpdateStaffRequest) (*Staff, error)
	ListRoles(ctx context.Context) ([]Role, error)
	ListPermissions(ctx context.Context) ([]Permission, error)
	ListRolePermissions(ctx context.Context, id uint) ([]Permission, error)
	CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error)
	CreatePosAccount(ctx context.Context, in CreatePosAccountInput) (*User, error)
	ListOrganizations(ctx context.Context) ([]Organization, error)
	// GetOrganization is `GET /internal/organizations/:id` (404 if unknown).
	GetOrganization(ctx context.Context, id uint) (*Organization, error)
	// ListOrgWideAccounts lists orgID's owner and service_center logins -
	// the accounts ListPosAccounts doesn't cover.
	ListOrgWideAccounts(ctx context.Context, orgID uint) ([]User, error)
	// ResetOrgWideAccountPassword hashes plaintext before it reaches the
	// repository (same pattern as ResetPosAccountPassword) and revokes the
	// account's refresh tokens. 404 for anything but an owner/service_center.
	ResetOrgWideAccountPassword(ctx context.Context, id uint, plaintext string) error
	// UpdateBranchStatus / UpdateDeviceStatus are the Shagan-team status
	// levers - a branch is active|inactive, a device active|inactive|revoked.
	// New shifts can only be opened on an active branch and device; a shift
	// already open is not interrupted. 404 for an unknown id.
	UpdateBranchStatus(ctx context.Context, id uint, status BranchStatus) error
	UpdateDeviceStatus(ctx context.Context, id uint, status DeviceStatus) error
	ListPosAccounts(ctx context.Context, orgID uint) ([]User, error)
	UpdateOrganizationStatus(ctx context.Context, id uint, status OrganizationStatus) error
	// UpdateOrganizationTimezone changes the IANA zone that defines the org's
	// calendar day (dashboard "today", report date ranges and daily buckets).
	// 400 for an unknown zone name, 404 for an unknown org. Applies to every
	// report from the next request - nothing stored is rewritten, since
	// timestamps are kept as instants.
	UpdateOrganizationTimezone(ctx context.Context, id uint, timezone string) error
	UpdatePosAccountStatus(ctx context.Context, id uint, status UserStatus) error
	// ResetPosAccountPassword hashes plaintext before it ever reaches the
	// repository - see Service.CreateAccount for the same pattern.
	ResetPosAccountPassword(ctx context.Context, id uint, plaintext string) error
}
