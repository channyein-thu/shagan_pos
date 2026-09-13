package identity

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type RepositoryImpl struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &RepositoryImpl{db: db}
}

var _ Repository = (*RepositoryImpl)(nil)

// Login backs `POST /auth/login`. Owner email+password login
func (r *RepositoryImpl) Login(ctx context.Context) (*Session, error) {
	return nil, common.ErrNotImplemented
}

// RefreshSession backs `POST /auth/refresh`.
func (r *RepositoryImpl) RefreshSession(ctx context.Context) (*Session, error) {
	return nil, common.ErrNotImplemented
}

// Logout backs `POST /auth/logout`. Revokes refresh token
func (r *RepositoryImpl) Logout(ctx context.Context) error {
	return common.ErrNotImplemented
}

// GetMe backs `GET /me`. Caller's identity + roles/permissions
func (r *RepositoryImpl) GetMe(ctx context.Context) (*User, error) {
	return nil, common.ErrNotImplemented
}

// UpdateMe backs `PATCH /me`. Locale preference, etc.
func (r *RepositoryImpl) UpdateMe(ctx context.Context, in UpdateMeRequest) (*User, error) {
	return nil, common.ErrNotImplemented
}

// VerifyManagerPIN backs `POST /auth/manager-pin/verify`. Short-lived elevation token for void/return/exchange approval
func (r *RepositoryImpl) VerifyManagerPIN(ctx context.Context) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// VerifyStaffPIN backs `POST /staff/:id/pin/verify`. Cashier PIN sign-on at a terminal
func (r *RepositoryImpl) VerifyStaffPIN(ctx context.Context, id uint) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// RegisterDevice backs `POST /devices/register`. Provisions a pos-type login seat
func (r *RepositoryImpl) RegisterDevice(ctx context.Context, in RegisterDeviceRequest) (*Device, error) {
	return nil, common.ErrNotImplemented
}

// ListDevices backs `GET /devices`.
func (r *RepositoryImpl) ListDevices(ctx context.Context) ([]Device, error) {
	return nil, common.ErrNotImplemented
}

// UpdateDevice backs `PATCH /devices/:id`.
func (r *RepositoryImpl) UpdateDevice(ctx context.Context, id uint, in UpdateDeviceRequest) (*Device, error) {
	return nil, common.ErrNotImplemented
}

// ListBranches backs `GET /branches`.
func (r *RepositoryImpl) ListBranches(ctx context.Context) ([]Branch, error) {
	return nil, common.ErrNotImplemented
}

// CreateBranch backs `POST /branches`.
func (r *RepositoryImpl) CreateBranch(ctx context.Context, in CreateBranchRequest) (*Branch, error) {
	return nil, common.ErrNotImplemented
}

// GetBranch backs `GET /branches/:id`.
func (r *RepositoryImpl) GetBranch(ctx context.Context, id uint) (*Branch, error) {
	return nil, common.ErrNotImplemented
}

// UpdateBranch backs `PATCH /branches/:id`.
func (r *RepositoryImpl) UpdateBranch(ctx context.Context, id uint, in UpdateBranchRequest) (*Branch, error) {
	return nil, common.ErrNotImplemented
}

// ListBranchStaff backs `GET /branches/:id/staff`.
func (r *RepositoryImpl) ListBranchStaff(ctx context.Context, id uint) ([]Staff, error) {
	return nil, common.ErrNotImplemented
}

// ListStaff backs `GET /staff`.
func (r *RepositoryImpl) ListStaff(ctx context.Context) ([]Staff, error) {
	return nil, common.ErrNotImplemented
}

// CreateStaff backs `POST /staff`. Ends in PIN set step
func (r *RepositoryImpl) CreateStaff(ctx context.Context, in CreateStaffRequest) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// GetStaff backs `GET /staff/:id`.
func (r *RepositoryImpl) GetStaff(ctx context.Context, id uint) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// UpdateStaff backs `PATCH /staff/:id`. Never a hard delete - deactivate only
func (r *RepositoryImpl) UpdateStaff(ctx context.Context, id uint, in UpdateStaffRequest) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// ListRoles backs `GET /roles`.
func (r *RepositoryImpl) ListRoles(ctx context.Context) ([]Role, error) {
	return nil, common.ErrNotImplemented
}

// ListPermissions backs `GET /permissions`.
func (r *RepositoryImpl) ListPermissions(ctx context.Context) ([]Permission, error) {
	return nil, common.ErrNotImplemented
}

// ListRolePermissions backs `GET /roles/:id/permissions`. Read the matrix
func (r *RepositoryImpl) ListRolePermissions(ctx context.Context, id uint) ([]Permission, error) {
	return nil, common.ErrNotImplemented
}

// CreateAccount backs `POST /internal/accounts`. Shagan-team-only: provisions a new tenant in one call.
func (r *RepositoryImpl) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	return nil, common.ErrNotImplemented
}
