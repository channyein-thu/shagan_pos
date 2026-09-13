package identity

import (
	"context"
	"time"
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
	RefreshSession(ctx context.Context) (*Session, error)
	Logout(ctx context.Context) error
	GetMe(ctx context.Context) (*User, error)
	UpdateMe(ctx context.Context, in UpdateMeRequest) (*User, error)
	VerifyManagerPIN(ctx context.Context) (*Staff, error)
	VerifyStaffPIN(ctx context.Context, id uint) (*Staff, error)
	RegisterDevice(ctx context.Context, in RegisterDeviceRequest) (*Device, error)
	ListDevices(ctx context.Context) ([]Device, error)
	UpdateDevice(ctx context.Context, id uint, in UpdateDeviceRequest) (*Device, error)
	ListBranches(ctx context.Context) ([]Branch, error)
	CreateBranch(ctx context.Context, in CreateBranchRequest) (*Branch, error)
	GetBranch(ctx context.Context, id uint) (*Branch, error)
	UpdateBranch(ctx context.Context, id uint, in UpdateBranchRequest) (*Branch, error)
	ListBranchStaff(ctx context.Context, id uint) ([]Staff, error)
	ListStaff(ctx context.Context) ([]Staff, error)
	CreateStaff(ctx context.Context, in CreateStaffRequest) (*Staff, error)
	GetStaff(ctx context.Context, id uint) (*Staff, error)
	UpdateStaff(ctx context.Context, id uint, in UpdateStaffRequest) (*Staff, error)
	ListRoles(ctx context.Context) ([]Role, error)
	ListPermissions(ctx context.Context) ([]Permission, error)
	ListRolePermissions(ctx context.Context, id uint) ([]Permission, error)
	CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error)
}
