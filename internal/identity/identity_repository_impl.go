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
	var session Session
	if err := r.db.WithContext(ctx).Where("refresh_hash = ?", refreshHash).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("session not found")
		}
		return nil, err
	}
	return &session, nil
}

// GetUserByID backs Service.RefreshSession's need for the user's OrgID.
func (r *RepositoryImpl) GetUserByID(ctx context.Context, id uint) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("user not found")
		}
		return nil, err
	}
	return &user, nil
}

// RevokeSession backs Service.RefreshSession (rotation) and Service.Logout.
func (r *RepositoryImpl) RevokeSession(ctx context.Context, sessionID uint) error {
	return r.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ?", sessionID).
		Update("revoked_at", time.Now()).
		Error
}

// UpdateMe backs `PATCH /me`. Only Name and Email are genuinely safe for a
// user to change about themselves:
//   - OrgID is deliberately never written - letting a user move themselves to
//     a different organization would be a tenant-isolation break.
//   - AccountType is a privilege level, not a profile field - changing it is
//     an admin action, not self-service.
//   - DeviceID is set by device pairing (CreateDevice), not a profile edit.
//   - CredentialHash: even though the DTO carries this field (see the TODO on
//     UpdateMeRequest), a real password change needs its own flow that
//     verifies the current password first - never just overwrite the hash.
func (r *RepositoryImpl) UpdateMe(ctx context.Context, userID uint, in UpdateMeRequest) (*User, error) {
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Email != nil {
		updates["email"] = *in.Email
	}

	if len(updates) > 0 {
		err := r.db.WithContext(ctx).Model(&User{}).Where("id = ?", userID).Updates(updates).Error
		if err != nil {
			if common.IsDuplicateError(err) {
				return nil, common.ConflictError("an account with this email already exists")
			}
			return nil, err
		}
	}

	return r.GetUserByID(ctx, userID)
}

// getDeviceInOrg fetches a device, scoped to orgID via its branch - same
// not-found-not-forbidden reasoning as getBranch/getStaff.
func (r *RepositoryImpl) getDeviceInOrg(ctx context.Context, orgID uint, id uint) (*Device, error) {
	var device Device
	err := r.db.WithContext(ctx).
		Where("id = ? AND branch_id IN (?)", id, r.orgBranchIDs(ctx, orgID)).
		First(&device).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("device not found")
		}
		return nil, err
	}
	return &device, nil
}

// CreateDevice backs `POST /devices`. Provisions a pos-type login seat
func (r *RepositoryImpl) CreateDevice(ctx context.Context, orgID uint, in CreateDeviceRequest) (*Device, error) {
	// the target branch must belong to this org, same check as CreateStaff.
	if _, err := r.GetBranch(ctx, orgID, in.BranchID); err != nil {
		return nil, err
	}

	device := Device{
		BranchID:   in.BranchID,
		Name:       in.Name,
		Status:     in.Status,
		LastSeenAt: time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// ListDevices backs `GET /devices`.
func (r *RepositoryImpl) ListDevices(ctx context.Context, orgID uint) ([]Device, error) {
	var devices []Device
	err := r.db.WithContext(ctx).
		Where("branch_id IN (?)", r.orgBranchIDs(ctx, orgID)).
		Find(&devices).Error
	if err != nil {
		return nil, err
	}
	return devices, nil
}

// UpdateDevice backs `PATCH /devices/:id`.
func (r *RepositoryImpl) UpdateDevice(ctx context.Context, orgID uint, id uint, in UpdateDeviceRequest) (*Device, error) {
	if _, err := r.getDeviceInOrg(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.BranchID != nil {
		// moving a device to a different branch - that branch must belong to
		// this org too, same check as CreateStaff/UpdateStaff.
		if _, err := r.GetBranch(ctx, orgID, *in.BranchID); err != nil {
			return nil, err
		}
		updates["branch_id"] = *in.BranchID
	}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}

	if len(updates) > 0 {
		if err := r.db.WithContext(ctx).Model(&Device{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return r.getDeviceInOrg(ctx, orgID, id)
}

// ListBranches backs `GET /branches`.
func (r *RepositoryImpl) ListBranches(ctx context.Context, orgID uint) ([]Branch, error) {
	var branches []Branch
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Find(&branches).Error; err != nil {
		return nil, err
	}
	return branches, nil
}

// CreateBranch backs `POST /branches`.
func (r *RepositoryImpl) CreateBranch(ctx context.Context, orgID uint, in CreateBranchRequest) (*Branch, error) {
	branch := Branch{
		OrgID:   orgID,
		Name:    in.Name,
		Status:  in.Status,
		Address: in.Address,
		Phone:   in.Phone,
	}
	if err := r.db.WithContext(ctx).Create(&branch).Error; err != nil {
		return nil, err
	}
	return &branch, nil
}

// GetBranch backs `GET /branches/:id`. Scoped to orgID - see the Repository
// interface doc on why a cross-org ID returns 404, not 403.
func (r *RepositoryImpl) GetBranch(ctx context.Context, orgID uint, id uint) (*Branch, error) {
	var branch Branch
	err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&branch).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("branch not found")
		}
		return nil, err
	}
	return &branch, nil
}

// UpdateBranch backs `PATCH /branches/:id`.
func (r *RepositoryImpl) UpdateBranch(ctx context.Context, orgID uint, id uint, in UpdateBranchRequest) (*Branch, error) {
	// confirms the branch exists AND belongs to orgID before touching anything
	if _, err := r.GetBranch(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if in.Address != nil {
		updates["address"] = *in.Address
	}
	if in.Phone != nil {
		updates["phone"] = *in.Phone
	}

	if len(updates) > 0 {
		if err := r.db.WithContext(ctx).Model(&Branch{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return r.GetBranch(ctx, orgID, id)
}

// ListBranchStaff backs `GET /branches/:id/staff`.
func (r *RepositoryImpl) ListBranchStaff(ctx context.Context, orgID uint, id uint) ([]Staff, error) {
	// confirms the branch belongs to orgID before revealing which staff work there
	if _, err := r.GetBranch(ctx, orgID, id); err != nil {
		return nil, err
	}

	var staff []Staff
	if err := r.db.WithContext(ctx).Where("branch_id = ?", id).Find(&staff).Error; err != nil {
		return nil, err
	}
	return staff, nil
}

// ListBranchManagers backs `GET /branches/:id/managers`. Same org/branch
// scoping as ListBranchStaff, additionally filtered to staff whose role
// grants permissionCode - via a subquery on role_permission/permissions,
// same shape as Service.VerifyManagerPIN's own permission check.
func (r *RepositoryImpl) ListBranchManagers(ctx context.Context, orgID uint, id uint, permissionCode string) ([]Staff, error) {
	if _, err := r.GetBranch(ctx, orgID, id); err != nil {
		return nil, err
	}

	grantedRoleIDs := r.db.WithContext(ctx).Model(&RolePermission{}).
		Where("permission_id = (?)", r.db.WithContext(ctx).Model(&Permission{}).Where("code = ?", permissionCode).Select("id")).
		Select("role_id")

	var staff []Staff
	err := r.db.WithContext(ctx).
		Where("branch_id = ? AND role IN (?)", id, grantedRoleIDs).
		Find(&staff).Error
	if err != nil {
		return nil, err
	}
	return staff, nil
}

// orgBranchIDs is a subquery selecting the IDs of every branch belonging to
// orgID - used to scope Staff (which has no org_id column of its own) to the
// caller's organization via its branch.
func (r *RepositoryImpl) orgBranchIDs(ctx context.Context, orgID uint) *gorm.DB {
	return r.db.WithContext(ctx).Model(&Branch{}).Where("org_id = ?", orgID).Select("id")
}

// ListStaff backs `GET /staff`.
func (r *RepositoryImpl) ListStaff(ctx context.Context, orgID uint, branchID *uint) ([]Staff, error) {
	var staff []Staff
	q := r.db.WithContext(ctx).Where("branch_id IN (?)", r.orgBranchIDs(ctx, orgID))
	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	}
	if err := q.Find(&staff).Error; err != nil {
		return nil, err
	}
	return staff, nil
}

// CreateStaff backs `POST /staff`. Ends in PIN set step
func (r *RepositoryImpl) CreateStaff(ctx context.Context, orgID uint, in CreateStaffRequest) (*Staff, error) {
	// the target branch must actually belong to this org, or a caller could
	// plant a staff row in someone else's branch by guessing its ID.
	if _, err := r.GetBranch(ctx, orgID, in.BranchID); err != nil {
		return nil, err
	}

	staff := Staff{
		BranchID: in.BranchID,
		Name:     in.Name,
		RoleID:   in.Role,
		PinHash:  in.Pin, // already hashed by Service.CreateStaff
		Phone:    in.Phone,
		Status:   in.Status,
	}
	if err := r.db.WithContext(ctx).Create(&staff).Error; err != nil {
		return nil, err
	}
	return &staff, nil
}

// GetStaff backs `GET /staff/:id`.
func (r *RepositoryImpl) GetStaff(ctx context.Context, orgID uint, id uint) (*Staff, error) {
	var staff Staff
	err := r.db.WithContext(ctx).
		Where("id = ? AND branch_id IN (?)", id, r.orgBranchIDs(ctx, orgID)).
		First(&staff).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("staff not found")
		}
		return nil, err
	}
	return &staff, nil
}

// UpdateStaff backs `PATCH /staff/:id`. Never a hard delete - deactivate only
func (r *RepositoryImpl) UpdateStaff(ctx context.Context, orgID uint, id uint, in UpdateStaffRequest) (*Staff, error) {
	if _, err := r.GetStaff(ctx, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.BranchID != nil {
		// moving staff to a different branch - that branch must belong to
		// this org too, same check as CreateStaff.
		if _, err := r.GetBranch(ctx, orgID, *in.BranchID); err != nil {
			return nil, err
		}
		updates["branch_id"] = *in.BranchID
	}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Role != nil {
		updates["role"] = *in.Role
	}
	if in.Pin != nil {
		updates["pin_hash"] = *in.Pin // already hashed by Service.UpdateStaff
	}
	if in.Phone != nil {
		updates["phone"] = *in.Phone
	}
	if in.Status != nil {
		updates["status"] = *in.Status
	}

	if len(updates) > 0 {
		if err := r.db.WithContext(ctx).Model(&Staff{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return r.GetStaff(ctx, orgID, id)
}

// ListRoles backs `GET /roles`. Roles are global and fixed for v1 (not
// per-org customizable - see the 09-12 permissions-model decision), so
// there's no org scoping here, unlike Branches/Staff/Devices.
func (r *RepositoryImpl) ListRoles(ctx context.Context) ([]Role, error) {
	var roles []Role
	if err := r.db.WithContext(ctx).Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// ListPermissions backs `GET /permissions`. Global, same reasoning as ListRoles.
func (r *RepositoryImpl) ListPermissions(ctx context.Context) ([]Permission, error) {
	var permissions []Permission
	if err := r.db.WithContext(ctx).Find(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

// ListRolePermissions backs `GET /roles/:id/permissions`. Read the matrix
func (r *RepositoryImpl) ListRolePermissions(ctx context.Context, id uint) ([]Permission, error) {
	var permissions []Permission
	err := r.db.WithContext(ctx).
		Where("id IN (?)", r.db.WithContext(ctx).Model(&RolePermission{}).Where("role_id = ?", id).Select("permission_id")).
		Find(&permissions).Error
	if err != nil {
		return nil, err
	}
	return permissions, nil
}

// CreateOrganization backs Service.CreateAccount's first step. Plain insert -
// GORM sets org.ID on the pointer it's given, for the caller to use in the
// rows it creates next. db is either r.db or an in-flight transaction handed
// down by the caller - see the Repository interface doc.
func (r *RepositoryImpl) CreateOrganization(db *gorm.DB, org *Organization) error {
	return db.Create(org).Error
}

// CreateUser backs both Service.CreateAccount (owner + service_center users)
// and CreatePosAccount below. Plain insert - GORM sets user.ID on the pointer
// it's given.
func (r *RepositoryImpl) CreateUser(db *gorm.DB, user *User) error {
	if err := db.Create(user).Error; err != nil {
		if common.IsDuplicateError(err) {
			return common.ConflictError("an account with this email already exists")
		}
		return err
	}
	return nil
}

// CreatePosAccount backs `POST /internal/accounts/pos`. Shagan-team-only:
// attaches a new pos-type User to an existing Organization + Device. Unlike
// CreateAccount, it never creates an Organization - in.DeviceID must already
// belong to in.OrgID, verified the same not-found-not-forbidden way as
// CreateDevice/UpdateDevice. By the time in.Password reaches here it is
// expected to already be a hash - see Service.CreatePosAccount. This is a
// single insert, so it just uses r.db directly - no transaction needed.
func (r *RepositoryImpl) CreatePosAccount(ctx context.Context, in CreatePosAccountInput) (*User, error) {
	device, err := r.getDeviceInOrg(ctx, in.OrgID, in.DeviceID)
	if err != nil {
		return nil, err
	}

	email := in.Email
	user := User{
		OrgID:          in.OrgID,
		AccountType:    AccountTypePos,
		DeviceID:       &device.ID,
		BranchID:       &device.BranchID,
		Email:          &email,
		CredentialHash: in.Password,
	}
	if err := r.CreateUser(r.db.WithContext(ctx), &user); err != nil {
		return nil, err
	}

	return &user, nil
}
