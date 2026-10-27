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
	return r.getDevice(r.db.WithContext(ctx), orgID, id)
}

// getDevice is getDeviceInOrg on db - the repository's connection or an
// in-flight transaction.
func (r *RepositoryImpl) getDevice(db *gorm.DB, orgID uint, id uint) (*Device, error) {
	var device Device
	err := db.
		Where("id = ? AND branch_id IN (?)", id, db.Session(&gorm.Session{NewDB: true}).Model(&Branch{}).Where("org_id = ?", orgID).Select("id")).
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
func (r *RepositoryImpl) CreateDevice(db *gorm.DB, orgID uint, in CreateDeviceRequest) (*Device, error) {
	// the target branch must belong to this org, same check as CreateStaff.
	if _, err := r.getBranch(db, orgID, in.BranchID); err != nil {
		return nil, err
	}

	device := Device{
		BranchID:   in.BranchID,
		Name:       in.Name,
		Status:     in.Status,
		LastSeenAt: time.Now(),
	}
	if err := db.Create(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// GetDevice backs Service.UpdateDevice's reactivation check.
func (r *RepositoryImpl) GetDevice(ctx context.Context, orgID uint, id uint) (*Device, error) {
	return r.getDeviceInOrg(ctx, orgID, id)
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
func (r *RepositoryImpl) UpdateDevice(db *gorm.DB, orgID uint, id uint, in UpdateDeviceRequest) (*Device, error) {
	if _, err := r.getDevice(db, orgID, id); err != nil {
		return nil, err
	}

	updates := map[string]any{}
	if in.BranchID != nil {
		// moving a device to a different branch - that branch must belong to
		// this org too, same check as CreateStaff/UpdateStaff.
		if _, err := r.getBranch(db, orgID, *in.BranchID); err != nil {
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
		if err := db.Model(&Device{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return r.getDevice(db, orgID, id)
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
func (r *RepositoryImpl) CreateBranch(db *gorm.DB, orgID uint, in CreateBranchRequest) (*Branch, error) {
	branch := Branch{
		OrgID:   orgID,
		Name:    in.Name,
		Status:  in.Status,
		Address: in.Address,
		Phone:   in.Phone,
	}
	if err := db.Create(&branch).Error; err != nil {
		return nil, err
	}
	return &branch, nil
}

// GetBranch backs `GET /branches/:id`. Scoped to orgID - see the Repository
// interface doc on why a cross-org ID returns 404, not 403.
func (r *RepositoryImpl) GetBranch(ctx context.Context, orgID uint, id uint) (*Branch, error) {
	return r.getBranch(r.db.WithContext(ctx), orgID, id)
}

// getBranch is GetBranch on db - the repository's connection or an in-flight
// transaction.
func (r *RepositoryImpl) getBranch(db *gorm.DB, orgID uint, id uint) (*Branch, error) {
	var branch Branch
	err := db.Where("id = ? AND org_id = ?", id, orgID).First(&branch).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("branch not found")
		}
		return nil, err
	}
	return &branch, nil
}

// UpdateBranch backs `PATCH /branches/:id`.
func (r *RepositoryImpl) UpdateBranch(db *gorm.DB, orgID uint, id uint, in UpdateBranchRequest) (*Branch, error) {
	// confirms the branch exists AND belongs to orgID before touching anything
	if _, err := r.getBranch(db, orgID, id); err != nil {
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
		if err := db.Model(&Branch{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return r.getBranch(db, orgID, id)
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

	grantedRoleIDs := r.db.WithContext(ctx).Model(&OrgRolePermission{}).
		Where("org_id = ? AND permission_id IN (?)", orgID, r.db.WithContext(ctx).Model(&Permission{}).Where("code = ?", permissionCode).Select("id")).
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

// UpdateStaffPinAttempts backs VerifyStaffPIN/VerifyManagerPIN's lockout
// bookkeeping. Uses a map (not a struct) so lockedUntil=nil actually clears
// pin_locked_until to NULL - GORM's struct-based Updates skips zero/nil
// fields, but a map update writes exactly what it's given.
func (r *RepositoryImpl) UpdateStaffPinAttempts(ctx context.Context, staffID uint, attempts int, lockedUntil *time.Time) error {
	return r.db.WithContext(ctx).Model(&Staff{}).Where("id = ?", staffID).
		Updates(map[string]any{
			"failed_pin_attempts": attempts,
			"pin_locked_until":    lockedUntil,
		}).Error
}

// ListRoles backs `GET /roles`. Roles are per organization: only orgID's own
// rows (never the global templates), system roles first, then by name.
func (r *RepositoryImpl) ListRoles(ctx context.Context, orgID uint) ([]Role, error) {
	var roles []Role
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).
		Order("is_system DESC, name ASC, id ASC").Find(&roles).Error; err != nil {
		return nil, err
	}
	return roles, nil
}

// GetRole returns common.NotFoundError for a role that exists but belongs to
// another org, same not-found-not-forbidden reasoning as GetBranch.
func (r *RepositoryImpl) GetRole(ctx context.Context, orgID uint, id uint) (*Role, error) {
	var role Role
	if err := r.db.WithContext(ctx).Where("id = ? AND org_id = ?", id, orgID).First(&role).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("role not found")
		}
		return nil, err
	}
	return &role, nil
}

// ListPermissions backs `GET /permissions`. The catalogue is global - every
// org picks from the same list - so there's no org scoping.
func (r *RepositoryImpl) ListPermissions(ctx context.Context) ([]Permission, error) {
	var permissions []Permission
	if err := r.db.WithContext(ctx).Find(&permissions).Error; err != nil {
		return nil, err
	}
	return permissions, nil
}

// ListRolePermissions backs `GET /roles/:id/permissions` and the permissions
// claim of a PIN token. Filtering the grants by orgID as well as roleID means
// a role id from another org (or none) yields no permissions, never theirs.
func (r *RepositoryImpl) ListRolePermissions(ctx context.Context, orgID uint, roleID uint) ([]Permission, error) {
	var permissions []Permission
	err := r.db.WithContext(ctx).
		Where("id IN (?)", r.db.WithContext(ctx).Model(&OrgRolePermission{}).
			Where("org_id = ? AND role_id = ?", orgID, roleID).Select("permission_id")).
		Order("id").
		Find(&permissions).Error
	if err != nil {
		return nil, err
	}
	return permissions, nil
}

// ListRoleGrantCodes backs the roles screen's POS / Back office switches in a
// single query instead of one per role.
func (r *RepositoryImpl) ListRoleGrantCodes(ctx context.Context, orgID uint) (map[uint][]string, error) {
	var rows []struct {
		RoleID uint
		Code   string
	}
	err := r.db.WithContext(ctx).Table("org_role_permission").
		Select("org_role_permission.role_id AS role_id, permissions.code AS code").
		Joins("JOIN permissions ON permissions.id = org_role_permission.permission_id").
		Where("org_role_permission.org_id = ?", orgID).
		Order("permissions.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint][]string)
	for _, row := range rows {
		out[row.RoleID] = append(out[row.RoleID], row.Code)
	}
	return out, nil
}

// CountStaffByRole backs the staff_count on each row of `GET /roles` (and the
// "in use" check before a delete). Staff have no org column, so they're scoped
// through their branch.
func (r *RepositoryImpl) CountStaffByRole(ctx context.Context, orgID uint) (map[uint]int, error) {
	var rows []struct {
		Role  uint
		Count int
	}
	err := r.db.WithContext(ctx).Model(&Staff{}).
		Select("role AS role, COUNT(*) AS count").
		Where("branch_id IN (?)", r.orgBranchIDs(ctx, orgID)).
		Group("role").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint]int, len(rows))
	for _, row := range rows {
		out[row.Role] = row.Count
	}
	return out, nil
}

// CreateRole backs `POST /roles`. db is the caller's transaction.
func (r *RepositoryImpl) CreateRole(db *gorm.DB, role *Role) error {
	if err := db.Create(role).Error; err != nil {
		if common.IsDuplicateError(err) {
			return common.ConflictError("a role with this name already exists")
		}
		return err
	}
	return nil
}

// UpdateRoleName backs `PATCH /roles/:id`'s rename. The code is deliberately
// never touched: staff and tokens refer to roles by id, and the code is what
// the migration and defaults key on.
func (r *RepositoryImpl) UpdateRoleName(db *gorm.DB, orgID uint, id uint, name string) error {
	res := db.Model(&Role{}).Where("id = ? AND org_id = ?", id, orgID).Update("name", name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return common.NotFoundError("role not found")
	}
	return nil
}

// ReplaceRoleGrants backs `PATCH /roles/:id`'s permission edit: the role ends
// up with exactly permissionIDs. Delete-then-insert inside the caller's
// transaction, so a failed insert never leaves the role half-stripped.
func (r *RepositoryImpl) ReplaceRoleGrants(db *gorm.DB, orgID uint, roleID uint, permissionIDs []uint) error {
	if err := db.Where("org_id = ? AND role_id = ?", orgID, roleID).Delete(&OrgRolePermission{}).Error; err != nil {
		return err
	}
	if len(permissionIDs) == 0 {
		return nil
	}
	grants := make([]OrgRolePermission, len(permissionIDs))
	for i, pid := range permissionIDs {
		grants[i] = OrgRolePermission{OrgID: orgID, RoleID: roleID, PermissionID: pid}
	}
	return db.Create(&grants).Error
}

// DeleteRole backs `DELETE /roles/:id`.
func (r *RepositoryImpl) DeleteRole(db *gorm.DB, orgID uint, id uint) error {
	res := db.Where("id = ? AND org_id = ?", id, orgID).Delete(&Role{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return common.NotFoundError("role not found")
	}
	return db.Where("org_id = ? AND role_id = ?", orgID, id).Delete(&OrgRolePermission{}).Error
}

// ProvisionDefaultRoles backs Service.CreateAccount - see the package-level
// ProvisionDefaultRoles.
func (r *RepositoryImpl) ProvisionDefaultRoles(db *gorm.DB, orgID uint) error {
	return ProvisionDefaultRoles(db, orgID)
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

// ListOrganizations backs `GET /internal/organizations`. Shagan-team-only:
// every org, no filter - there's no tenant JWT to scope this from.
func (r *RepositoryImpl) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var orgs []Organization
	if err := r.db.WithContext(ctx).Find(&orgs).Error; err != nil {
		return nil, err
	}
	return orgs, nil
}

// GetOrganization backs Login/RefreshSession's suspension check.
func (r *RepositoryImpl) GetOrganization(ctx context.Context, id uint) (*Organization, error) {
	var org Organization
	if err := r.db.WithContext(ctx).First(&org, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("organization not found")
		}
		return nil, err
	}
	return &org, nil
}

// ListPosAccounts backs `GET /internal/accounts/pos`. Shagan-team-only:
// orgID comes from a query param, not a token.
func (r *RepositoryImpl) ListPosAccounts(ctx context.Context, orgID uint) ([]User, error) {
	var users []User
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND account_type = ?", orgID, AccountTypePos).
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

// UpdateOrganizationStatus backs `PATCH /internal/organizations/:id/status`.
func (r *RepositoryImpl) UpdateOrganizationStatus(ctx context.Context, id uint, status OrganizationStatus) error {
	return r.db.WithContext(ctx).Model(&Organization{}).Where("id = ?", id).Update("status", status).Error
}

// UpdateOrganizationTimezone backs `PATCH /internal/organizations/:id/timezone`.
func (r *RepositoryImpl) UpdateOrganizationTimezone(ctx context.Context, id uint, timezone string) error {
	return r.db.WithContext(ctx).Model(&Organization{}).Where("id = ?", id).Update("timezone", timezone).Error
}

// getPosAccount fetches a User, scoped to AccountTypePos - shared by
// UpdatePosAccountStatus/ResetPosAccountPassword so neither can be used to
// silently disable/reset an owner or service_center login instead.
func (r *RepositoryImpl) getPosAccount(ctx context.Context, id uint) (*User, error) {
	var user User
	err := r.db.WithContext(ctx).Where("id = ? AND account_type = ?", id, AccountTypePos).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("pos account not found")
		}
		return nil, err
	}
	return &user, nil
}

// UpdatePosAccountStatus backs `PATCH /internal/accounts/pos/:id/status`.
func (r *RepositoryImpl) UpdatePosAccountStatus(ctx context.Context, id uint, status UserStatus) error {
	if _, err := r.getPosAccount(ctx, id); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("status", status).Error
}

// ResetPosAccountPassword backs `POST /internal/accounts/pos/:id/reset-password`.
// credentialHash is already hashed - see Service.ResetPosAccountPassword.
func (r *RepositoryImpl) ResetPosAccountPassword(ctx context.Context, id uint, credentialHash string) error {
	if _, err := r.getPosAccount(ctx, id); err != nil {
		return err
	}
	return r.setPasswordAndRevokeSessions(ctx, id, credentialHash)
}

// setPasswordAndRevokeSessions writes the new hash and revokes every live
// refresh token of the user, atomically - a password reset that leaves old
// sessions refreshable would not actually lock out whoever knew the old
// password. (An already-issued access token still lives out its short TTL.)
func (r *RepositoryImpl) setPasswordAndRevokeSessions(ctx context.Context, userID uint, credentialHash string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", userID).Update("credential_hash", credentialHash).Error; err != nil {
			return err
		}
		return tx.Model(&Session{}).
			Where("user_id = ? AND revoked_at IS NULL", userID).
			Update("revoked_at", time.Now()).Error
	})
}

// ListOrgWideAccounts backs `GET /internal/accounts/org-wide`.
func (r *RepositoryImpl) ListOrgWideAccounts(ctx context.Context, orgID uint) ([]User, error) {
	var users []User
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND account_type IN ?", orgID, []AccountType{AccountTypeOwner, AccountTypeServiceCenter}).
		Order("id").
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

// ResetOrgWideAccountPassword backs
// `POST /internal/accounts/org-wide/:id/reset-password`.
func (r *RepositoryImpl) ResetOrgWideAccountPassword(ctx context.Context, id uint, credentialHash string) error {
	var user User
	err := r.db.WithContext(ctx).
		Where("id = ? AND account_type IN ?", id, []AccountType{AccountTypeOwner, AccountTypeServiceCenter}).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NotFoundError("account not found")
		}
		return err
	}
	return r.setPasswordAndRevokeSessions(ctx, id, credentialHash)
}

// UpdateBranchStatusInternal backs `PATCH /internal/branches/:id/status`.
func (r *RepositoryImpl) UpdateBranchStatusInternal(db *gorm.DB, id uint, status BranchStatus) error {
	res := db.Model(&Branch{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return common.NotFoundError("branch not found")
	}
	return nil
}

// UpdateDeviceStatusInternal backs `PATCH /internal/devices/:id/status`.
func (r *RepositoryImpl) UpdateDeviceStatusInternal(db *gorm.DB, id uint, status DeviceStatus) error {
	res := db.Model(&Device{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return common.NotFoundError("device not found")
	}
	return nil
}

// GetBranchAnyOrg is GetBranch without the org scope, for the Shagan-team
// status lever (which has an id but no tenant token). NotFound only if the id
// doesn't exist at all.
func (r *RepositoryImpl) GetBranchAnyOrg(db *gorm.DB, id uint) (*Branch, error) {
	var branch Branch
	if err := db.Where("id = ?", id).First(&branch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NotFoundError("branch not found")
		}
		return nil, err
	}
	return &branch, nil
}

// GetDeviceAnyOrg is GetBranchAnyOrg for a device; it also returns the org the
// device belongs to through its branch.
func (r *RepositoryImpl) GetDeviceAnyOrg(db *gorm.DB, id uint) (*Device, uint, error) {
	var device Device
	if err := db.Where("id = ?", id).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, common.NotFoundError("device not found")
		}
		return nil, 0, err
	}
	var branch Branch
	if err := db.Where("id = ?", device.BranchID).First(&branch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, common.NotFoundError("device not found")
		}
		return nil, 0, err
	}
	return &device, branch.OrgID, nil
}
