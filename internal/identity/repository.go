package identity

import (
	"context"

	"gorm.io/gorm"

	"shagan_pos/internal/common"
)

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Login backs `POST /auth/login`. Owner email+password login
func (r *Repository) Login(ctx context.Context) (*Session, error) {
	return nil, common.ErrNotImplemented
}

// RefreshSession backs `POST /auth/refresh`.
func (r *Repository) RefreshSession(ctx context.Context) (*Session, error) {
	return nil, common.ErrNotImplemented
}

// Logout backs `POST /auth/logout`. Revokes refresh token
func (r *Repository) Logout(ctx context.Context) error {
	return common.ErrNotImplemented
}

// GetMe backs `GET /me`. Caller's identity + roles/permissions
func (r *Repository) GetMe(ctx context.Context) (*User, error) {
	return nil, common.ErrNotImplemented
}

// UpdateMe backs `PATCH /me`. Locale preference, etc.
func (r *Repository) UpdateMe(ctx context.Context, in User) (*User, error) {
	return nil, common.ErrNotImplemented
}

// VerifyManagerPIN backs `POST /auth/manager-pin/verify`. Short-lived elevation token for void/return/exchange approval
func (r *Repository) VerifyManagerPIN(ctx context.Context) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// VerifyStaffPIN backs `POST /staff/:id/pin/verify`. Cashier PIN sign-on at a terminal
func (r *Repository) VerifyStaffPIN(ctx context.Context, id uint) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// RegisterDevice backs `POST /devices/register`. Provisions a pos-type login seat
func (r *Repository) RegisterDevice(ctx context.Context, in Device) (*Device, error) {
	return nil, common.ErrNotImplemented
}

// ListDevices backs `GET /devices`.
func (r *Repository) ListDevices(ctx context.Context) ([]Device, error) {
	return nil, common.ErrNotImplemented
}

// UpdateDevice backs `PATCH /devices/:id`.
func (r *Repository) UpdateDevice(ctx context.Context, id uint, in Device) (*Device, error) {
	return nil, common.ErrNotImplemented
}

// ListBranches backs `GET /branches`.
func (r *Repository) ListBranches(ctx context.Context) ([]Branch, error) {
	return nil, common.ErrNotImplemented
}

// CreateBranch backs `POST /branches`.
func (r *Repository) CreateBranch(ctx context.Context, in Branch) (*Branch, error) {
	return nil, common.ErrNotImplemented
}

// GetBranch backs `GET /branches/:id`.
func (r *Repository) GetBranch(ctx context.Context, id uint) (*Branch, error) {
	return nil, common.ErrNotImplemented
}

// UpdateBranch backs `PATCH /branches/:id`.
func (r *Repository) UpdateBranch(ctx context.Context, id uint, in Branch) (*Branch, error) {
	return nil, common.ErrNotImplemented
}

// ListBranchStaff backs `GET /branches/:id/staff`.
func (r *Repository) ListBranchStaff(ctx context.Context, id uint) ([]Staff, error) {
	return nil, common.ErrNotImplemented
}

// ListStaff backs `GET /staff`.
func (r *Repository) ListStaff(ctx context.Context) ([]Staff, error) {
	return nil, common.ErrNotImplemented
}

// CreateStaff backs `POST /staff`. Ends in PIN set step
func (r *Repository) CreateStaff(ctx context.Context, in Staff) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// GetStaff backs `GET /staff/:id`.
func (r *Repository) GetStaff(ctx context.Context, id uint) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// UpdateStaff backs `PATCH /staff/:id`. Never a hard delete - deactivate only
func (r *Repository) UpdateStaff(ctx context.Context, id uint, in Staff) (*Staff, error) {
	return nil, common.ErrNotImplemented
}

// ListRoles backs `GET /roles`.
func (r *Repository) ListRoles(ctx context.Context) ([]Role, error) {
	return nil, common.ErrNotImplemented
}

// ListPermissions backs `GET /permissions`.
func (r *Repository) ListPermissions(ctx context.Context) ([]Permission, error) {
	return nil, common.ErrNotImplemented
}

// ListRolePermissions backs `GET /roles/:id/permissions`. Read the matrix
func (r *Repository) ListRolePermissions(ctx context.Context, id uint) ([]Permission, error) {
	return nil, common.ErrNotImplemented
}

// CreateAccount backs `POST /internal/accounts`. Shagan-team-only: provisions
// a new tenant (Organization + owner User + a default Branch) in one call.
func (r *Repository) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	return nil, common.ErrNotImplemented
}
