package identity

import (
	"context"
	"time"

	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/subscription"
)

// Subscriptions is what identity needs from subscription: the plan summary on
// GET /me, usage for the organization list's over_limit flag, the limit check
// that runs in the same transaction as a branch / device insert or
// reactivation, and the history row of a new org's trial.
// subscription.Service already satisfies this signature - no adapter needed.
type Subscriptions interface {
	Summary(ctx context.Context, orgID uint, now time.Time) (*subscription.Summary, error)
	UsageByOrg(ctx context.Context) (map[uint]subscription.Usage, error)
	EnforceLimit(tx *gorm.DB, orgID uint, kind subscription.LimitKind, now time.Time) error
	RecordChange(tx *gorm.DB, c *subscription.OrgPlanChange) error
}

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
	// GetMe returns the caller plus their organization's subscription.
	GetMe(ctx context.Context, userID uint) (*MeResult, error)
	UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error)
	// VerifyManagerPIN backs `POST /staff/:id/manager-pin/verify`. Same
	// org/branch scoping and generic-401 reasoning as VerifyStaffPIN below,
	// plus a check that the target staff's role actually grants in.Permission
	// - momentary single-action elevation only, not a backoffice sign-in (use
	// VerifyStaffPIN for that).
	VerifyManagerPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyManagerPINRequest) (*VerifyManagerPINResult, error)
	// VerifyApprover is the PIN check behind a discount approval: another staff
	// member of branchID enters their PIN on the till to approve something the
	// seller can't. Same credential rules as VerifyManagerPIN - shared lockout
	// counter, the staff must belong to branchID, and every failure except
	// lockout is the same generic 401 - but it issues no token, and it returns
	// the approver's permission codes instead of judging them: whether they are
	// enough depends on whether the approver is also the seller, which only the
	// caller knows. It lives here (not in sales) so the lockout counter stays in
	// one place.
	VerifyApprover(ctx context.Context, orgID uint, branchID uint, staffID uint, pin string) (*Staff, []string, error)
	// VerifyStaffPIN backs `POST /staff/:id/pin/verify`. branchID is the
	// caller's own branch (nil for an owner/service_center token, which isn't
	// branch-locked) - when set, the staff being verified must belong to that
	// same branch, same not-found-not-forbidden reasoning as GetStaff, folded
	// into the same generic 401 as a wrong PIN so a terminal can't enumerate
	// staff IDs by probing.
	VerifyStaffPIN(ctx context.Context, orgID uint, branchID *uint, id uint, in VerifyStaffPINRequest) (*VerifyStaffPINResult, error)
	// CreateDevice, CreateBranch and the UpdateDevice / UpdateBranch /
	// UpdateBranchStatus / UpdateDeviceStatus calls that reactivate something
	// are bound by the organization's plan: adding or reactivating one more
	// ACTIVE branch / device than the plan (plus extras) allows is a 403
	// plan_limit_reached. The count and the write share one transaction, under a
	// lock on the organization, so two parallel calls can't both pass. Inactive
	// and revoked ones don't count, and nothing ever switches an existing one off.
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
	// ListRoles is `GET /roles`: the org's roles with their staff counts and
	// POS / Back office switches, behind a synthetic, non-editable Owner row.
	ListRoles(ctx context.Context, orgID uint) ([]RoleSummary, error)
	// ListPermissions is `GET /permissions`: the catalogue grouped by
	// category (pos, then backoffice), in catalogue order.
	ListPermissions(ctx context.Context) ([]PermissionGroup, error)
	// ListRolePermissions is `GET /roles/:id/permissions` - the grants of one
	// of the org's roles (404 for another org's).
	ListRolePermissions(ctx context.Context, orgID uint, id uint) ([]Permission, error)
	// CreateRole, UpdateRole and DeleteRole are Owner-only (the handlers sit
	// behind RequireOrgAdmin) and audited as entity "role". actorUserID is the
	// authenticated org-wide account, never client input. Changes reach staff
	// at their next PIN sign-in - a token already issued keeps the permissions
	// it was minted with.
	//
	// CreateRole: 400 for a bad name or unknown permission code, 409 if the
	// org already has a role with that name.
	CreateRole(ctx context.Context, orgID uint, actorUserID uint, in CreateRoleRequest) (*RoleDetail, error)
	// UpdateRole: 403 for the synthetic Owner row (id 0), 404 for another
	// org's role, 400 for an unknown code, 409 if it would leave the org with
	// no role holding access_pos_portal or the new name is taken.
	UpdateRole(ctx context.Context, orgID uint, actorUserID uint, id uint, in UpdateRoleRequest) (*RoleDetail, error)
	// DeleteRole: 403 for the Owner row, 404 for another org's role, 409 for a
	// system role (Manager, Cashier) or one any staff member still holds.
	DeleteRole(ctx context.Context, orgID uint, actorUserID uint, id uint) error
	CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error)
	CreatePosAccount(ctx context.Context, in CreatePosAccountInput) (*User, error)
	ListOrganizations(ctx context.Context) ([]OrganizationSummary, error)
	// GetOrganization is `GET /internal/organizations/:id` (404 if unknown).
	GetOrganization(ctx context.Context, id uint) (*OrganizationSummary, error)
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
