package api

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
)

type IdentityAPI struct {
	service   identity.Interface
	jwtSecret []byte
}

func NewIdentityAPI(db *gorm.DB, jwtSecret []byte, accessTokenTTL, refreshTokenTTL, staffPINTokenTTL, managerPINTokenTTL time.Duration) *IdentityAPI {
	return &IdentityAPI{
		service:   identity.NewService(identity.NewRepository(db), audit.NewRepository(db), db, jwtSecret, accessTokenTTL, refreshTokenTTL, staffPINTokenTTL, managerPINTokenTTL),
		jwtSecret: jwtSecret,
	}
}

// RegisterPublicRoutes mounts the two routes that must work without an
// access token - login (that's the whole point) and refresh (the whole
// point of which is obtaining a new access token when you don't have a
// valid one anymore). Neither can sit behind middleware.Auth.
func (a *IdentityAPI) RegisterPublicRoutes(rg *gin.RouterGroup) {
	rg.POST("/auth/login", a.Login)
	rg.POST("/auth/refresh", a.RefreshSession)
}

func (a *IdentityAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/auth/logout", a.Logout)
	rg.GET("/me", a.GetMe)
	rg.PATCH("/me", a.UpdateMe)
	rg.POST("/staff/:id/manager-pin/verify", a.VerifyManagerPIN)
	rg.POST("/staff/:id/pin/verify", a.VerifyStaffPIN)
	// Provisioning/management writes are org-wide-account only (owner /
	// service_center): a till's POS-device token never reaches them, manager
	// PIN or not - a manager's staff access is view-only (WORKFLOWS section 4).
	orgAdmin := middleware.RequireOrgAdmin()
	rg.POST("/devices", orgAdmin, a.CreateDevice)
	rg.GET("/devices", a.ListDevices)
	rg.PATCH("/devices/:id", orgAdmin, a.UpdateDevice)
	rg.GET("/branches", a.ListBranches)
	rg.POST("/branches", orgAdmin, a.CreateBranch)
	rg.GET("/branches/:id", a.GetBranch)
	rg.PATCH("/branches/:id", orgAdmin, a.UpdateBranch)
	// The staff list is read by the till's PIN pad, so view_staff only
	// restricts a caller using Back Office. Adding and editing staff is Owner /
	// Service Center, or a till's Back Office with manage_staff - which is
	// confined to its own branch and to roles it could itself hold (see
	// guardTillStaffChange).
	viewStaff := middleware.RequireViewPermission(a.jwtSecret, "view_staff")
	manageStaff := middleware.RequireBackOfficePermission(a.jwtSecret, "manage_staff")
	rg.GET("/branches/:id/staff", viewStaff, a.ListBranchStaff)
	rg.GET("/branches/:id/managers", a.ListBranchManagers)
	rg.GET("/staff", viewStaff, a.ListStaff)
	rg.POST("/staff", manageStaff, a.CreateStaff)
	rg.GET("/staff/:id", viewStaff, a.GetStaff)
	rg.PATCH("/staff/:id", manageStaff, a.UpdateStaff)
	// Role & access management is Owner / Service Center only and never
	// grantable. Changes reach staff at their next PIN sign-in.
	rg.GET("/roles", orgAdmin, a.ListRoles)
	rg.POST("/roles", orgAdmin, a.CreateRole)
	rg.PATCH("/roles/:id", orgAdmin, a.UpdateRole)
	rg.DELETE("/roles/:id", orgAdmin, a.DeleteRole)
	rg.GET("/permissions", orgAdmin, a.ListPermissions)
	rg.GET("/roles/:id/permissions", orgAdmin, a.ListRolePermissions)
}

// RegisterInternalRoutes mounts routes meant only for Shagan's own internal
// tooling, protected by middleware.InternalAuth (a shared secret) instead of
// the normal staff-PIN/JWT auth - see cmd/main.go's "/internal" group.
func (a *IdentityAPI) RegisterInternalRoutes(rg *gin.RouterGroup) {
	rg.POST("/accounts", a.CreateAccount)
	rg.POST("/accounts/pos", a.CreatePosAccount)
	rg.POST("/branches", a.CreateBranchInternal)
	rg.POST("/devices", a.CreateDeviceInternal)
	rg.GET("/organizations", a.ListOrganizations)
	rg.GET("/organizations/:id", a.GetOrganizationInternal)
	rg.PATCH("/organizations/:id/status", a.UpdateOrganizationStatus)
	rg.PATCH("/organizations/:id/timezone", a.UpdateOrganizationTimezone)
	rg.GET("/branches", a.ListBranchesInternal)
	rg.GET("/devices", a.ListDevicesInternal)
	rg.GET("/accounts/org-wide", a.ListOrgWideAccounts)
	rg.POST("/accounts/org-wide/:id/reset-password", a.ResetOrgWideAccountPassword)
	rg.PATCH("/branches/:id/status", a.UpdateBranchStatusInternal)
	rg.PATCH("/devices/:id/status", a.UpdateDeviceStatusInternal)
	rg.GET("/accounts/pos", a.ListPosAccounts)
	rg.PATCH("/accounts/pos/:id/status", a.UpdatePosAccountStatus)
	rg.POST("/accounts/pos/:id/reset-password", a.ResetPosAccountPassword)
}

// requireOrgIDQuery reads a required org_id query param - the internal
// portal has no tenant JWT to scope from, so org_id is passed explicitly and
// trusted because the caller already holds the shared X-Internal-Key.
func requireOrgIDQuery(c *gin.Context) (uint, bool) {
	val, err := strconv.ParseUint(c.Query("org_id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("org_id query param is required"))
		return 0, false
	}
	return uint(val), true
}

// Login handles `POST /auth/login`. Owner email+password login
func (a *IdentityAPI) Login(c *gin.Context) {
	var in identity.LoginRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.Login(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// RefreshSession handles `POST /auth/refresh`.
func (a *IdentityAPI) RefreshSession(c *gin.Context) {
	var in identity.RefreshRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.RefreshSession(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// Logout handles `POST /auth/logout`. Revokes refresh token
func (a *IdentityAPI) Logout(c *gin.Context) {
	var in identity.LogoutRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.Logout(c.Request.Context(), in); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GetMe handles `GET /me`. Caller's identity + roles/permissions
func (a *IdentityAPI) GetMe(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	result, err := a.service.GetMe(c.Request.Context(), userID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateMe handles `PATCH /me`. Locale preference, etc.
func (a *IdentityAPI) UpdateMe(c *gin.Context) {
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	var in identity.UpdateMeRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateMe(c.Request.Context(), userID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// VerifyManagerPIN handles `POST /staff/:id/manager-pin/verify`. Short-lived
// elevation token for void/return/manual-discount approval - :id is the
// staff being asked to approve, in.Permission is the specific permission
// this approval is for (e.g. "approve_void"). Not for backoffice sign-in -
// see VerifyStaffPIN for that.
func (a *IdentityAPI) VerifyManagerPIN(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.VerifyManagerPINRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	var branchID *uint
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = &bID
	}
	result, err := a.service.VerifyManagerPIN(c.Request.Context(), orgID, branchID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// VerifyStaffPIN handles `POST /staff/:id/pin/verify`. Cashier PIN sign-on at a terminal
func (a *IdentityAPI) VerifyStaffPIN(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.VerifyStaffPINRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	var branchID *uint
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = &bID
	}
	result, err := a.service.VerifyStaffPIN(c.Request.Context(), orgID, branchID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateDevice handles `POST /devices`. Provisions a pos-type login seat
func (a *IdentityAPI) CreateDevice(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var in identity.CreateDeviceRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateDevice(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListDevices handles `GET /devices`.
func (a *IdentityAPI) ListDevices(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListDevices(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateDevice handles `PATCH /devices/:id`.
func (a *IdentityAPI) UpdateDevice(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateDeviceRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateDevice(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// requireOrgID reads the authenticated caller's org from context, writing a
// 401 itself if it's somehow missing (shouldn't happen behind middleware.Auth,
// but a handler must never silently fall back to an unscoped query).
func requireOrgID(c *gin.Context) (uint, bool) {
	orgID, ok := middleware.OrgIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
	}
	return orgID, ok
}

// ListBranches handles `GET /branches`.
func (a *IdentityAPI) ListBranches(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListBranches(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateBranch handles `POST /branches`.
func (a *IdentityAPI) CreateBranch(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var in identity.CreateBranchRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateBranch(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetBranch handles `GET /branches/:id`.
func (a *IdentityAPI) GetBranch(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetBranch(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateBranch handles `PATCH /branches/:id`.
func (a *IdentityAPI) UpdateBranch(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateBranchRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateBranch(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListBranchStaff handles `GET /branches/:id/staff`.
func (a *IdentityAPI) ListBranchStaff(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	// A POS-device token (a till) may only list its own branch's staff.
	branchID, ok := scopedBranchID(c, uint(idVal))
	if !ok {
		return
	}
	result, err := a.service.ListBranchStaff(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListBranchManagers handles `GET /branches/:id/managers`. permission is a
// required query param (a permission code like "approve_void") - staff
// eligible to approve one action aren't eligible to approve every action, so
// "manager" alone isn't a well-defined filter (see identity.Repository.ListBranchManagers).
func (a *IdentityAPI) ListBranchManagers(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	permission := c.Query("permission")
	if permission == "" {
		common.HandleError(c, common.BadRequestError("permission query param is required"))
		return
	}
	branchID, ok := scopedBranchID(c, uint(idVal))
	if !ok {
		return
	}
	result, err := a.service.ListBranchManagers(c.Request.Context(), orgID, branchID, permission)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListStaff handles `GET /staff`. Restricted to the caller's own branch when
// the caller's token carries one (a pos device) - org-wide for owner/service_center.
func (a *IdentityAPI) ListStaff(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var branchID *uint
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = &bID
	}
	result, err := a.service.ListStaff(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateStaff handles `POST /staff`. Ends in PIN set step
func (a *IdentityAPI) CreateStaff(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var in identity.CreateStaffRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if !a.guardTillStaffChange(c, orgID, in.BranchID, in.Role) {
		return
	}
	result, err := a.service.CreateStaff(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetStaff handles `GET /staff/:id`.
func (a *IdentityAPI) GetStaff(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetStaff(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateStaff handles `PATCH /staff/:id`. Never a hard delete - deactivate
// only. Writes an audit entry - see identity.Interface.UpdateStaff's doc.
func (a *IdentityAPI) UpdateStaff(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorUserID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateStaffRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if isTillCaller(c) {
		// a till can only touch staff of its own branch, and never someone
		// whose role holds more than it does
		target, err := a.service.GetStaff(c.Request.Context(), orgID, uint(idVal))
		if err != nil {
			common.HandleError(c, err)
			return
		}
		roles := []uint{target.RoleID}
		if in.Role != nil {
			roles = append(roles, *in.Role)
		}
		if !a.guardTillStaffChange(c, orgID, target.BranchID, roles...) {
			return
		}
		if in.BranchID != nil && !a.guardTillStaffChange(c, orgID, *in.BranchID) {
			return
		}
	}
	result, err := a.service.UpdateStaff(c.Request.Context(), orgID, actorUserID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// isTillCaller is true for a POS-device token (a till), false for an Owner /
// Service Center acting directly.
func isTillCaller(c *gin.Context) bool {
	accountType, ok := middleware.AccountTypeFromContext(c)
	return ok && accountType == middleware.AccountTypePos
}

// guardTillStaffChange applies the extra limits on a till (Back Office with
// manage_staff) adding or editing staff; an Owner / Service Center is not
// limited. A till may only act on its own branch, and may only involve roles
// whose every permission it holds itself - otherwise manage_staff would let
// someone mint a Manager, or reset a Manager's PIN and sign in as them. Writes
// the error response and returns false when refused.
func (a *IdentityAPI) guardTillStaffChange(c *gin.Context, orgID uint, branchID uint, roleIDs ...uint) bool {
	if !isTillCaller(c) {
		return true
	}
	if _, ok := scopedBranchID(c, branchID); !ok {
		return false
	}
	held := middleware.StaffPermissions(c)
	for _, roleID := range roleIDs {
		perms, err := a.service.ListRolePermissions(c.Request.Context(), orgID, roleID)
		if err != nil {
			common.HandleError(c, err)
			return false
		}
		for _, p := range perms {
			if !slices.Contains(held, p.Code) {
				common.HandleError(c, common.ForbiddenError("cannot use a role that holds permissions you do not hold"))
				return false
			}
		}
	}
	return true
}

// roleIDParam reads :id of a role route. The literal "owner" is the synthetic
// Owner row of GET /roles - it has no id, so it maps to 0, which the service
// refuses as fixed.
func roleIDParam(c *gin.Context) (uint, bool) {
	if c.Param("id") == identity.RoleCodeOwner {
		return 0, true
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return 0, false
	}
	return uint(idVal), true
}

// ListRoles handles `GET /roles` (Owner / Service Center only): the org's
// roles behind a synthetic, non-editable Owner row.
func (a *IdentityAPI) ListRoles(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListRoles(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListPermissions handles `GET /permissions`: the catalogue grouped by
// category (pos, backoffice).
func (a *IdentityAPI) ListPermissions(c *gin.Context) {
	result, err := a.service.ListPermissions(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListRolePermissions handles `GET /roles/:id/permissions`: this org's grants
// for one of its roles (404 for another org's).
func (a *IdentityAPI) ListRolePermissions(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, ok := roleIDParam(c)
	if !ok {
		return
	}
	result, err := a.service.ListRolePermissions(c.Request.Context(), orgID, idVal)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateRole handles `POST /roles`. Audited as entity "role".
func (a *IdentityAPI) CreateRole(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorUserID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	var in identity.CreateRoleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateRole(c.Request.Context(), orgID, actorUserID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateRole handles `PATCH /roles/:id`: rename and/or replace the role's
// permissions. 403 for the Owner row, 409 if it would leave nobody able to
// use the till.
func (a *IdentityAPI) UpdateRole(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorUserID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	idVal, ok := roleIDParam(c)
	if !ok {
		return
	}
	var in identity.UpdateRoleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateRole(c.Request.Context(), orgID, actorUserID, idVal, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteRole handles `DELETE /roles/:id`. 409 for a system role or one still
// held by staff.
func (a *IdentityAPI) DeleteRole(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorUserID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	idVal, ok := roleIDParam(c)
	if !ok {
		return
	}
	if err := a.service.DeleteRole(c.Request.Context(), orgID, actorUserID, idVal); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// CreateAccount handles `POST /internal/accounts`. Shagan-team-only: provisions
// a new tenant (Organization + owner User + service_center User) in one call.
func (a *IdentityAPI) CreateAccount(c *gin.Context) {
	var in identity.CreateAccountInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateAccount(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// CreatePosAccount handles `POST /internal/accounts/pos`. Shagan-team-only:
// attaches a new pos-type User to an existing Organization + Device.
func (a *IdentityAPI) CreatePosAccount(c *gin.Context) {
	var in identity.CreatePosAccountInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreatePosAccount(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListOrganizations handles `GET /internal/organizations`. Shagan-team-only:
// every org, no filter - there's no tenant JWT to scope this from.
func (a *IdentityAPI) ListOrganizations(c *gin.Context) {
	result, err := a.service.ListOrganizations(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateOrganizationStatus handles
// `PATCH /internal/organizations/:id/status`. Shagan-team-only: suspends or
// reactivates a whole tenant.
func (a *IdentityAPI) UpdateOrganizationStatus(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateOrganizationStatusRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.UpdateOrganizationStatus(c.Request.Context(), uint(idVal), in.Status); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateOrganizationTimezone handles
// `PATCH /internal/organizations/:id/timezone`. Shagan-team-only: sets the
// IANA zone that defines the tenant's calendar day (reports' "today").
func (a *IdentityAPI) UpdateOrganizationTimezone(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateOrganizationTimezoneRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.UpdateOrganizationTimezone(c.Request.Context(), uint(idVal), in.Timezone); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GetOrganizationInternal handles `GET /internal/organizations/:id`.
// Shagan-team-only: one tenant, so the portal needn't list them all and filter.
func (a *IdentityAPI) GetOrganizationInternal(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetOrganization(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListOrgWideAccounts handles `GET /internal/accounts/org-wide`.
// Shagan-team-only: the org's owner and service_center logins (org_id is a
// query param, same as ListPosAccounts).
func (a *IdentityAPI) ListOrgWideAccounts(c *gin.Context) {
	orgID, ok := requireOrgIDQuery(c)
	if !ok {
		return
	}
	result, err := a.service.ListOrgWideAccounts(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResetOrgWideAccountPassword handles
// `POST /internal/accounts/org-wide/:id/reset-password`. Shagan-team-only.
func (a *IdentityAPI) ResetOrgWideAccountPassword(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.ResetOrgWideAccountPasswordRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.ResetOrgWideAccountPassword(c.Request.Context(), uint(idVal), in.Password); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateBranchStatusInternal handles `PATCH /internal/branches/:id/status`.
func (a *IdentityAPI) UpdateBranchStatusInternal(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateBranchStatusRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.UpdateBranchStatus(c.Request.Context(), uint(idVal), in.Status); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateDeviceStatusInternal handles `PATCH /internal/devices/:id/status`.
func (a *IdentityAPI) UpdateDeviceStatusInternal(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdateDeviceStatusRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.UpdateDeviceStatus(c.Request.Context(), uint(idVal), in.Status); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListBranchesInternal handles `GET /internal/branches`. Shagan-team-only:
// reuses the same Service.ListBranches the tenant-facing GET /branches uses,
// just with org_id read from a query param instead of the caller's own JWT.
func (a *IdentityAPI) ListBranchesInternal(c *gin.Context) {
	orgID, ok := requireOrgIDQuery(c)
	if !ok {
		return
	}
	result, err := a.service.ListBranches(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListDevicesInternal handles `GET /internal/devices`. Shagan-team-only:
// reuses the same Service.ListDevices the tenant-facing GET /devices uses.
func (a *IdentityAPI) ListDevicesInternal(c *gin.Context) {
	orgID, ok := requireOrgIDQuery(c)
	if !ok {
		return
	}
	result, err := a.service.ListDevices(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListPosAccounts handles `GET /internal/accounts/pos`. Shagan-team-only:
// every pos-type User in the given org.
func (a *IdentityAPI) ListPosAccounts(c *gin.Context) {
	orgID, ok := requireOrgIDQuery(c)
	if !ok {
		return
	}
	result, err := a.service.ListPosAccounts(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdatePosAccountStatus handles `PATCH /internal/accounts/pos/:id/status`.
// Shagan-team-only: suspends or reactivates a single pos-terminal login,
// independent of the underlying Device's own status.
func (a *IdentityAPI) UpdatePosAccountStatus(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.UpdatePosAccountStatusRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.UpdatePosAccountStatus(c.Request.Context(), uint(idVal), in.Status); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ResetPosAccountPassword handles
// `POST /internal/accounts/pos/:id/reset-password`. Shagan-team-only.
func (a *IdentityAPI) ResetPosAccountPassword(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in identity.ResetPosAccountPasswordRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if err := a.service.ResetPosAccountPassword(c.Request.Context(), uint(idVal), in.Password); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// CreateBranchInternal handles `POST /internal/branches`. Shagan-team-only:
// lets internal tooling create a Branch for an org right after CreateAccount,
// before that org's owner has ever logged in - reuses the same
// Service.CreateBranch as the authenticated POST /branches, just reading
// OrgID from the request body instead of the JWT.
func (a *IdentityAPI) CreateBranchInternal(c *gin.Context) {
	var in identity.CreateBranchInternalRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateBranch(c.Request.Context(), in.OrgID, identity.CreateBranchRequest{
		Name:    in.Name,
		Status:  in.Status,
		Address: in.Address,
		Phone:   in.Phone,
	})
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// CreateDeviceInternal handles `POST /internal/devices`. Shagan-team-only:
// same reasoning as CreateBranchInternal, for devices - reuses
// Service.CreateDevice, which already verifies BranchID belongs to OrgID.
func (a *IdentityAPI) CreateDeviceInternal(c *gin.Context) {
	var in identity.CreateDeviceInternalRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateDevice(c.Request.Context(), in.OrgID, identity.CreateDeviceRequest{
		BranchID: in.BranchID,
		Name:     in.Name,
		Status:   in.Status,
	})
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
