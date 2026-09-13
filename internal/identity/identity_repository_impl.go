package identity

import (
	"context"
	"errors"
	"time"

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

// GetUserByEmail backs Service.Login's credential lookup.
func (r *RepositoryImpl) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("user not found")
		}
		return nil, err
	}
	return &user, nil
}

// CreateSession backs Service.Login's session creation.
func (r *RepositoryImpl) CreateSession(ctx context.Context, userID uint, refreshHash string, expiresAt time.Time) (*Session, error) {
	session := Session{
		UserID:      userID,
		RefreshHash: refreshHash,
		ExpiresAt:   expiresAt,
	}
	if err := r.db.WithContext(ctx).Create(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// GetSessionByRefreshHash backs Service.RefreshSession's token lookup.
func (r *RepositoryImpl) GetSessionByRefreshHash(ctx context.Context, refreshHash string) (*Session, error) {
	return nil, common.ErrNotImplemented
}

// GetUserByID backs Service.RefreshSession's need for the user's OrgID.
func (r *RepositoryImpl) GetUserByID(ctx context.Context, id uint) (*User, error) {
	return nil, common.ErrNotImplemented
}

// RevokeSession backs Service.RefreshSession (rotation) and Service.Logout.
func (r *RepositoryImpl) RevokeSession(ctx context.Context, sessionID uint) error {
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

// CreateAccount backs `POST /internal/accounts`. Shagan-team-only: provisions
// a new tenant (Organization + owner User + a default Branch) in one
// transaction, so a failure partway through never leaves an orphaned
// Organization with no owner. By the time in.OwnerPassword reaches here it is
// expected to already be a hash - see Service.CreateAccount.
func (r *RepositoryImpl) CreateAccount(ctx context.Context, in CreateAccountInput) (*CreateAccountResult, error) {
	var result CreateAccountResult

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		org := Organization{Name: in.OrganizationName}
		if err := tx.Create(&org).Error; err != nil {
			return err
		}

		email := in.OwnerEmail
		owner := User{
			OrgID:          org.ID,
			AccountType:    AccountTypeOwner,
			Email:          &email,
			CredentialHash: in.OwnerPassword,
		}
		if err := tx.Create(&owner).Error; err != nil {
			if common.IsDuplicateError(err) {
				return common.ConflictError("an account with this email already exists")
			}
			return err
		}

		branch := Branch{
			OrgID:  org.ID,
			Name:   in.BranchName,
			Status: BranchStatusActive,
		}
		if err := tx.Create(&branch).Error; err != nil {
			return err
		}

		result = CreateAccountResult{Organization: org, Owner: owner, Branch: branch}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}
