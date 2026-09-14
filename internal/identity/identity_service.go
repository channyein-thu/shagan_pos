package identity

import "context"

// Interface defines the identity domain's use cases.
type Interface interface {
	Login(ctx context.Context, in LoginRequest) (*SessionResult, error)
	RefreshSession(ctx context.Context, in RefreshRequest) (*SessionResult, error)
	Logout(ctx context.Context, in LogoutRequest) error
	GetMe(ctx context.Context, userID uint) (*User, error)
	UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error)
	VerifyManagerPIN(ctx context.Context) (*Staff, error)
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
	ListStaff(ctx context.Context, orgID uint) ([]Staff, error)
	CreateStaff(ctx context.Context, orgID uint, in CreateStaffRequest) (*Staff, error)
	GetStaff(ctx context.Context, orgID uint, id uint) (*Staff, error)
	UpdateStaff(ctx context.Context, orgID uint, id uint, in UpdateStaffRequest) (*Staff, error)
	ListRoles(ctx context.Context) ([]Role, error)
	ListPermissions(ctx context.Context) ([]Permission, error)
	ListRolePermissions(ctx context.Context, id uint) ([]Permission, error)
	CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error)
	CreatePosAccount(ctx context.Context, in CreatePosAccountInput) (*User, error)
}
