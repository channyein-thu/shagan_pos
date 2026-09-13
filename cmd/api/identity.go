package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
)

type IdentityAPI struct {
	service identity.Interface
}

func NewIdentityAPI(db *gorm.DB) *IdentityAPI {
	return &IdentityAPI{service: identity.NewService(identity.NewRepository(db))}
}

func (a *IdentityAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/auth/login", a.Login)
	rg.POST("/auth/refresh", a.RefreshSession)
	rg.POST("/auth/logout", a.Logout)
	rg.GET("/me", a.GetMe)
	rg.PATCH("/me", a.UpdateMe)
	rg.POST("/auth/manager-pin/verify", a.VerifyManagerPIN)
	rg.POST("/staff/:id/pin/verify", a.VerifyStaffPIN)
	rg.POST("/devices/register", a.RegisterDevice)
	rg.GET("/devices", a.ListDevices)
	rg.PATCH("/devices/:id", a.UpdateDevice)
	rg.GET("/branches", a.ListBranches)
	rg.POST("/branches", a.CreateBranch)
	rg.GET("/branches/:id", a.GetBranch)
	rg.PATCH("/branches/:id", a.UpdateBranch)
	rg.GET("/branches/:id/staff", a.ListBranchStaff)
	rg.GET("/staff", a.ListStaff)
	rg.POST("/staff", a.CreateStaff)
	rg.GET("/staff/:id", a.GetStaff)
	rg.PATCH("/staff/:id", a.UpdateStaff)
	rg.GET("/roles", a.ListRoles)
	rg.GET("/permissions", a.ListPermissions)
	rg.GET("/roles/:id/permissions", a.ListRolePermissions)
}

// Login handles `POST /auth/login`. Owner email+password login
func (a *IdentityAPI) Login(c *gin.Context) {
	result, err := a.service.Login(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// RefreshSession handles `POST /auth/refresh`.
func (a *IdentityAPI) RefreshSession(c *gin.Context) {
	result, err := a.service.RefreshSession(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// Logout handles `POST /auth/logout`. Revokes refresh token
func (a *IdentityAPI) Logout(c *gin.Context) {
	if err := a.service.Logout(c.Request.Context()); err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// GetMe handles `GET /me`. Caller's identity + roles/permissions
func (a *IdentityAPI) GetMe(c *gin.Context) {
	result, err := a.service.GetMe(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateMe handles `PATCH /me`. Locale preference, etc.
func (a *IdentityAPI) UpdateMe(c *gin.Context) {
	var in identity.User
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.UpdateMe(c.Request.Context(), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// VerifyManagerPIN handles `POST /auth/manager-pin/verify`. Short-lived elevation token for void/return/exchange approval
func (a *IdentityAPI) VerifyManagerPIN(c *gin.Context) {
	result, err := a.service.VerifyManagerPIN(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// VerifyStaffPIN handles `POST /staff/:id/pin/verify`. Cashier PIN sign-on at a terminal
func (a *IdentityAPI) VerifyStaffPIN(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.VerifyStaffPIN(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// RegisterDevice handles `POST /devices/register`. Provisions a pos-type login seat
func (a *IdentityAPI) RegisterDevice(c *gin.Context) {
	var in identity.Device
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.RegisterDevice(c.Request.Context(), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListDevices handles `GET /devices`.
func (a *IdentityAPI) ListDevices(c *gin.Context) {
	result, err := a.service.ListDevices(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateDevice handles `PATCH /devices/:id`.
func (a *IdentityAPI) UpdateDevice(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	var in identity.Device
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.UpdateDevice(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListBranches handles `GET /branches`.
func (a *IdentityAPI) ListBranches(c *gin.Context) {
	result, err := a.service.ListBranches(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateBranch handles `POST /branches`.
func (a *IdentityAPI) CreateBranch(c *gin.Context) {
	var in identity.Branch
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.CreateBranch(c.Request.Context(), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetBranch handles `GET /branches/:id`.
func (a *IdentityAPI) GetBranch(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.GetBranch(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateBranch handles `PATCH /branches/:id`.
func (a *IdentityAPI) UpdateBranch(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	var in identity.Branch
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.UpdateBranch(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListBranchStaff handles `GET /branches/:id/staff`.
func (a *IdentityAPI) ListBranchStaff(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.ListBranchStaff(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListStaff handles `GET /staff`.
func (a *IdentityAPI) ListStaff(c *gin.Context) {
	result, err := a.service.ListStaff(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateStaff handles `POST /staff`. Ends in PIN set step
func (a *IdentityAPI) CreateStaff(c *gin.Context) {
	var in identity.Staff
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.CreateStaff(c.Request.Context(), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetStaff handles `GET /staff/:id`.
func (a *IdentityAPI) GetStaff(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.GetStaff(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateStaff handles `PATCH /staff/:id`. Never a hard delete - deactivate only
func (a *IdentityAPI) UpdateStaff(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	var in identity.Staff
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.UpdateStaff(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListRoles handles `GET /roles`.
func (a *IdentityAPI) ListRoles(c *gin.Context) {
	result, err := a.service.ListRoles(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListPermissions handles `GET /permissions`.
func (a *IdentityAPI) ListPermissions(c *gin.Context) {
	result, err := a.service.ListPermissions(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListRolePermissions handles `GET /roles/:id/permissions`. Read the matrix
func (a *IdentityAPI) ListRolePermissions(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.ListRolePermissions(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}
