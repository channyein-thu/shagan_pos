package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
)

type IdentityAPI struct {
	service identity.Interface
}

func NewIdentityAPI(db *gorm.DB, jwtSecret []byte, accessTokenTTL, refreshTokenTTL, staffPINTokenTTL, managerPINTokenTTL time.Duration) *IdentityAPI {
	return &IdentityAPI{
		service: identity.NewService(identity.NewRepository(db), db, jwtSecret, accessTokenTTL, refreshTokenTTL, staffPINTokenTTL, managerPINTokenTTL),
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
	rg.POST("/devices", a.CreateDevice)
	rg.GET("/devices", a.ListDevices)
	rg.PATCH("/devices/:id", a.UpdateDevice)
	rg.GET("/branches", a.ListBranches)
	rg.POST("/branches", a.CreateBranch)
	rg.GET("/branches/:id", a.GetBranch)
	rg.PATCH("/branches/:id", a.UpdateBranch)
	rg.GET("/branches/:id/staff", a.ListBranchStaff)
	rg.GET("/branches/:id/managers", a.ListBranchManagers)
	rg.GET("/staff", a.ListStaff)
	rg.POST("/staff", a.CreateStaff)
	rg.GET("/staff/:id", a.GetStaff)
	rg.PATCH("/staff/:id", a.UpdateStaff)
	rg.GET("/roles", a.ListRoles)
	rg.GET("/permissions", a.ListPermissions)
	rg.GET("/roles/:id/permissions", a.ListRolePermissions)
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
	rg.PATCH("/organizations/:id/status", a.UpdateOrganizationStatus)
	rg.GET("/branches", a.ListBranchesInternal)
	rg.GET("/devices", a.ListDevicesInternal)
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
	result, err := a.service.ListBranchStaff(c.Request.Context(), orgID, uint(idVal))
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
	result, err := a.service.ListBranchManagers(c.Request.Context(), orgID, uint(idVal), permission)
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

// UpdateStaff handles `PATCH /staff/:id`. Never a hard delete - deactivate only
func (a *IdentityAPI) UpdateStaff(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
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
	result, err := a.service.UpdateStaff(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListRoles handles `GET /roles`.
func (a *IdentityAPI) ListRoles(c *gin.Context) {
	result, err := a.service.ListRoles(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListPermissions handles `GET /permissions`.
func (a *IdentityAPI) ListPermissions(c *gin.Context) {
	result, err := a.service.ListPermissions(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListRolePermissions handles `GET /roles/:id/permissions`. Read the matrix
func (a *IdentityAPI) ListRolePermissions(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ListRolePermissions(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
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
