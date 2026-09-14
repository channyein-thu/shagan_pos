package identity

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Repository defines the identity domain's persistence operations.
type Repository interface {
	// GetUserByEmail returns common.NotFoundError when no user has that email -
	// Service.Login relies on that specific status to fold "unknown email" and
	// "wrong password" into the same generic response.
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	// CreateSession persists a new session for userID. refreshHash is the
	// refresh token's hash, never the plaintext - see Service.Login.
	CreateSession(ctx context.Context, userID uint, refreshHash string, expiresAt time.Time) (*Session, error)
	// GetSessionByRefreshHash returns common.NotFoundError when no session has
	// that hash - Service.RefreshSession relies on that specific status to
	// fold "unknown token" into the same generic response as an expired one.
	GetSessionByRefreshHash(ctx context.Context, refreshHash string) (*Session, error)
	// GetUserByID backs RefreshSession's need for the user's OrgID (Session
	// doesn't carry it) when minting a new access token.
	GetUserByID(ctx context.Context, id uint) (*User, error)
	// RevokeSession marks a session as revoked so its refresh token can never
	// be used again - called on both logout and successful refresh rotation.
	RevokeSession(ctx context.Context, sessionID uint) error
	// UpdateMe applies the caller's own self-service profile changes and
	// returns the updated User. There's no separate "GetMe" method here -
	// Service.GetMe just calls GetUserByID, since fetching your own profile
	// by ID is the exact same query as fetching anyone else's.
	UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error)
	VerifyManagerPIN(ctx context.Context) (*Staff, error)
	// Devices are scoped to orgID via their branch, same as Staff.
	CreateDevice(ctx context.Context, orgID uint, in CreateDeviceRequest) (*Device, error)
	ListDevices(ctx context.Context, orgID uint) ([]Device, error)
	UpdateDevice(ctx context.Context, orgID uint, id uint, in UpdateDeviceRequest) (*Device, error)
	// Branches are always scoped to orgID (the authenticated caller's own
	// organization) - GetBranch/UpdateBranch/ListBranchStaff return
	// common.NotFoundError for a branch that exists but belongs to a
	// different org, same as one that doesn't exist at all, so a caller can
	// never distinguish "not mine" from "doesn't exist" by probing IDs.
	ListBranches(ctx context.Context, orgID uint) ([]Branch, error)
	CreateBranch(ctx context.Context, orgID uint, in CreateBranchRequest) (*Branch, error)
	GetBranch(ctx context.Context, orgID uint, id uint) (*Branch, error)
	UpdateBranch(ctx context.Context, orgID uint, id uint, in UpdateBranchRequest) (*Branch, error)
	ListBranchStaff(ctx context.Context, orgID uint, id uint) ([]Staff, error)
	// Staff are scoped to orgID via their branch (Staff has no org_id column
	// of its own) - same not-found-not-forbidden reasoning as branches.
	ListStaff(ctx context.Context, orgID uint) ([]Staff, error)
	CreateStaff(ctx context.Context, orgID uint, in CreateStaffRequest) (*Staff, error)
	GetStaff(ctx context.Context, orgID uint, id uint) (*Staff, error)
	UpdateStaff(ctx context.Context, orgID uint, id uint, in UpdateStaffRequest) (*Staff, error)
	ListRoles(ctx context.Context) ([]Role, error)
	ListPermissions(ctx context.Context) ([]Permission, error)
	ListRolePermissions(ctx context.Context, id uint) ([]Permission, error)
	// CreateOrganization and CreateUser are the two plain inserts
	// Service.CreateAccount composes inside one db.Transaction call to
	// provision a new tenant. Each just persists what it's given via db and
	// sets the row's ID on the passed pointer - db is either the repository's
	// normal connection or an in-flight transaction, so the same method works
	// standalone or composed. The transaction boundary and the decision of
	// what to create belong to the service, not here.
	CreateOrganization(db *gorm.DB, org *Organization) error
	// CreateUser returns common.ConflictError if user.Email is already taken.
	CreateUser(db *gorm.DB, user *User) error
	// CreatePosAccount returns common.NotFoundError if in.DeviceID doesn't
	// belong to in.OrgID - same not-found-not-forbidden reasoning as branches.
	CreatePosAccount(ctx context.Context, in CreatePosAccountInput) (*User, error)
}
