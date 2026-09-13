package identity

import "context"

// Interface defines the identity domain's use cases.
type Interface interface {
	Login(ctx context.Context, in LoginRequest) (*LoginResult, error)
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
