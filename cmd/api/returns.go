package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/returns"
	"shagan_pos/internal/sales"
)

type ReturnsAPI struct {
	service   returns.Interface
	jwtSecret []byte
}

func NewReturnsAPI(db *gorm.DB, jwtSecret []byte) *ReturnsAPI {
	return &ReturnsAPI{
		service: returns.NewService(
			returns.NewRepository(db),
			identity.NewRepository(db),
			sales.NewRepository(db),
			inventory.NewRepository(db),
			audit.NewRepository(db),
			db,
		),
		jwtSecret: jwtSecret,
	}
}

func (a *ReturnsAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// Void also accepts an Owner / Service Center bearer token acting
	// directly with no PIN (the Owner has no Staff record) - see VoidSale.
	rg.POST("/sales/:id/void", middleware.RequireStaffTokenOrOrgAdmin(a.jwtSecret), a.VoidSale)
	rg.GET("/voids", a.ListVoids)
	rg.POST("/returns", middleware.RequireStaffToken(a.jwtSecret), a.CreateReturn)
	rg.GET("/returns", a.ListReturns)
	rg.GET("/returns/:id", a.GetReturn)
	rg.POST("/exchanges", middleware.RequireStaffToken(a.jwtSecret), a.CreateExchange)
	rg.GET("/exchanges", a.ListExchanges)
	rg.GET("/exchanges/:id", a.GetExchange)
}

// VoidSale handles `POST /sales/:id/void`. Reverses the entire sale - see
// returns.Interface's doc. A POS-device caller requires X-Staff-Token; the
// acting staff needs approve_void themselves, OR an optional
// X-Manager-Approval-Token grants it instead (see middleware.ManagerApproved)
// - same StaffHasPermission-or-ManagerApproved shape as sales.CreateSale's
// manual discount check. An Owner / Service Center bearer token may void
// directly with no staff token and no approval: they're the approver
// (recorded as approved_by_user_id). The "shift must still be open" rule
// applies to them too.
func (a *ReturnsAPI) VoidSale(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in returns.VoidSaleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	var actor returns.Actor
	if staffID, ok := middleware.StaffIDFromContext(c); ok {
		actor = returns.Actor{
			StaffID: staffID,
			CanApprove: middleware.StaffHasPermission(c, "approve_void") ||
				middleware.ManagerApproved(c, a.jwtSecret, "approve_void"),
		}
	} else if userID, ok := middleware.UserIDFromContext(c); ok {
		actor = returns.Actor{UserID: userID, CanApprove: true}
	} else {
		common.HandleError(c, common.UnauthorizedError("missing staff token"))
		return
	}
	result, err := a.service.VoidSale(c.Request.Context(), orgID, actor, id, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListVoids handles `GET /voids`. Restricted to the caller's own branch
// when the caller's token carries one - org-wide for owner/service_center,
// same reasoning as inventory.ListStockLevels. Optional ?branch_id=
// (owner/service_center only).
func (a *ReturnsAPI) ListVoids(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListVoids(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateReturn handles `POST /returns`. Also writes return_items. Requires
// X-Staff-Token; the acting staff needs approve_return themselves, OR an
// optional X-Manager-Approval-Token grants it instead - same shape as
// VoidSale.
func (a *ReturnsAPI) CreateReturn(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	var in returns.CreateReturnRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	// PosUserID is the till's own bearer account - how the service finds the
	// till's open shift to stamp on the record (see returns.Actor).
	posUserID, _ := middleware.UserIDFromContext(c)
	actor := returns.Actor{
		StaffID:   staffID,
		PosUserID: posUserID,
		CanApprove: middleware.StaffHasPermission(c, "approve_return") ||
			middleware.ManagerApproved(c, a.jwtSecret, "approve_return"),
	}
	result, err := a.service.CreateReturn(c.Request.Context(), orgID, actor, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListReturns handles `GET /returns`. Same scoping as ListVoids.
func (a *ReturnsAPI) ListReturns(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListReturns(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetReturn handles `GET /returns/:id`.
func (a *ReturnsAPI) GetReturn(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetReturn(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateExchange handles `POST /exchanges`. Also writes exchange_items.
// Requires X-Staff-Token; the acting staff needs approve_exchange
// themselves, OR an optional X-Manager-Approval-Token grants it instead -
// same shape as VoidSale.
func (a *ReturnsAPI) CreateExchange(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	var in returns.CreateExchangeRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	// PosUserID is the till's own bearer account - how the service finds the
	// till's open shift to stamp on the record (see returns.Actor).
	posUserID, _ := middleware.UserIDFromContext(c)
	actor := returns.Actor{
		StaffID:   staffID,
		PosUserID: posUserID,
		CanApprove: middleware.StaffHasPermission(c, "approve_exchange") ||
			middleware.ManagerApproved(c, a.jwtSecret, "approve_exchange"),
	}
	result, err := a.service.CreateExchange(c.Request.Context(), orgID, actor, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListExchanges handles `GET /exchanges`. Same scoping as ListVoids.
func (a *ReturnsAPI) ListExchanges(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListExchanges(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetExchange handles `GET /exchanges/:id`.
func (a *ReturnsAPI) GetExchange(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetExchange(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
