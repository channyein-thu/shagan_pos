package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/shift"
)

type ShiftAPI struct {
	service   shift.Interface
	jwtSecret []byte
}

func NewShiftAPI(db *gorm.DB, jwtSecret []byte) *ShiftAPI {
	return &ShiftAPI{service: shift.NewService(shift.NewRepository(db)), jwtSecret: jwtSecret}
}

func (a *ShiftAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// RequireStaffToken proves *which staff member* is acting, for the
	// endpoints where that identity is the point (opening/closing their own
	// shift, attributing a drawer event/expense to themselves) - see
	// shift.ExpenseActor and cmd/api/shift.go's handlers below.
	requireStaff := middleware.RequireStaffToken(a.jwtSecret)
	// access_backoffice (Manager-only) gates the force-close override - see
	// ForceCloseShift.
	requireManager := middleware.RequirePermission(a.jwtSecret, "access_backoffice")
	rg.POST("/shifts", requireStaff, a.OpenShift)
	rg.GET("/shifts/current", a.GetCurrentShift)
	rg.GET("/shifts/:id", a.GetShift)
	rg.POST("/shifts/:id/close", requireStaff, a.CloseShift)
	rg.POST("/shifts/:id/force-close", requireManager, a.ForceCloseShift)
	rg.GET("/shifts/:id/summary", a.GetShiftSummary)
	rg.GET("/shifts/:id/reconciliations", a.ListShiftReconciliations)
	rg.POST("/drawer-events", requireStaff, a.CreateDrawerEvent)
	rg.GET("/drawer-events", a.ListDrawerEvents)
	rg.GET("/expenses", a.ListExpenses)
	// Expenses also accept an Owner / Service Center bearer token acting
	// directly, with no staff token (the Owner has no Staff record).
	staffOrOrgAdmin := middleware.RequireStaffTokenOrOrgAdmin(a.jwtSecret)
	rg.POST("/expenses", staffOrOrgAdmin, a.CreateExpense)
	rg.PATCH("/expenses/:id", staffOrOrgAdmin, a.UpdateExpense)
	rg.DELETE("/expenses/:id", staffOrOrgAdmin, a.DeleteExpense)
}

func shiftAccessScope(c *gin.Context) (shift.AccessScope, bool) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return shift.AccessScope{}, false
	}
	scope := shift.AccessScope{OrgID: orgID}
	if branchID, ok := middleware.BranchIDFromContext(c); ok {
		scope.BranchID = &branchID
	}
	return scope, true
}

// requireStaffID reads the acting staff member's ID from the verified
// X-Staff-Token, as set by middleware.RequireStaffToken - a handler must
// never fall back to a client-supplied staff id, that's the exact gap this
// exists to close.
func requireStaffID(c *gin.Context) (uint, bool) {
	staffID, ok := middleware.StaffIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing staff token"))
	}
	return staffID, ok
}

// expenseActor builds the authorization the repository checks before
// letting UpdateExpense/DeleteExpense touch another staff member's expense.
// A staff caller (X-Staff-Token) may manage any expense only with
// access_backoffice, the Manager permission (see internal/seed/seed.go); an
// org-wide Owner / Service Center caller (no staff token - see
// middleware.RequireStaffTokenOrOrgAdmin) acts as themself and may manage
// any expense in their org. Writes a 401 itself and returns ok=false if
// neither identity is present.
func expenseActor(c *gin.Context) (shift.ExpenseActor, bool) {
	if staffID, ok := middleware.StaffIDFromContext(c); ok {
		return shift.ExpenseActor{StaffID: staffID, CanManageAny: middleware.StaffHasPermission(c, "access_backoffice")}, true
	}
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing staff token"))
		return shift.ExpenseActor{}, false
	}
	return shift.ExpenseActor{UserID: userID, CanManageAny: true}, true
}

// OpenShift handles `POST /shifts`. Open shift with opening float - always
// belongs to whichever staff member's X-Staff-Token PIN'd in, never a
// client-supplied staff id (that same staff is who CloseShift later
// requires to close it).
func (a *ShiftAPI) OpenShift(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	var in shift.OpenShiftRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	in.OrgID = orgID
	in.StaffID = staffID
	// A POS account is pinned to one branch in its signed access token. Owner
	// and service-center accounts have no branch claim and must submit one;
	// either way the repository still scopes it to the authenticated org.
	if branchID, ok := middleware.BranchIDFromContext(c); ok {
		in.BranchID = branchID
	}
	result, err := a.service.OpenShift(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetCurrentShift handles `GET /shifts/current`.
func (a *ShiftAPI) GetCurrentShift(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	userID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	result, err := a.service.GetCurrentShift(c.Request.Context(), orgID, userID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetShift handles `GET /shifts/:id`.
func (a *ShiftAPI) GetShift(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetShift(c.Request.Context(), scope, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CloseShift handles `POST /shifts/:id/close`. Writes reconciliation row(s)
// as a side effect - closing_cash is the physically-counted drawer amount,
// reason is required only when it doesn't match the system's expected cash.
func (a *ShiftAPI) CloseShift(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in shift.CloseShiftRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CloseShift(c.Request.Context(), scope, uint(idVal), staffID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ForceCloseShift handles `POST /shifts/:id/force-close`. Manager-only
// (access_backoffice) override of CloseShift's "same staff who opened it"
// rule - for when that staff is genuinely unavailable (e.g. called in sick)
// and the till would otherwise stay locked open indefinitely. reason is
// always required here, not just on a cash mismatch.
func (a *ShiftAPI) ForceCloseShift(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in shift.CloseShiftRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.ForceCloseShift(c.Request.Context(), scope, uint(idVal), staffID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetShiftSummary handles `GET /shifts/:id/summary`. Printable summary
func (a *ShiftAPI) GetShiftSummary(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetShiftSummary(c.Request.Context(), scope, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListShiftReconciliations handles `GET /shifts/:id/reconciliations`. Per-method breakdown
func (a *ShiftAPI) ListShiftReconciliations(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ListShiftReconciliations(c.Request.Context(), scope, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateDrawerEvent handles `POST /drawer-events`. Attributed to whichever
// staff member's X-Staff-Token is calling, never a client-supplied staff id.
// Opening the drawer without an attached sale (sale_id omitted) needs the
// open_drawer_no_sale permission - own, or an X-Manager-Approval-Token
// granting it instead.
func (a *ShiftAPI) CreateDrawerEvent(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	var in shift.CreateDrawerEventRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	in.StaffID = staffID
	canOpenDrawerNoSale := middleware.StaffHasPermission(c, "open_drawer_no_sale") ||
		middleware.ManagerApproved(c, a.jwtSecret, "open_drawer_no_sale")
	result, err := a.service.CreateDrawerEvent(c.Request.Context(), scope, canOpenDrawerNoSale, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListDrawerEvents handles `GET /drawer-events`.
func (a *ShiftAPI) ListDrawerEvents(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	result, err := a.service.ListDrawerEvents(c.Request.Context(), scope)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListExpenses handles `GET /expenses`.
func (a *ShiftAPI) ListExpenses(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	result, err := a.service.ListExpenses(c.Request.Context(), scope)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateExpense handles `POST /expenses`. Always attributed to whoever is
// calling, never a client-supplied id: the staff member whose X-Staff-Token
// is presented, or - for an Owner / Service Center bearer token acting
// directly - that account (created_by_user_id, no staff id). An org-wide
// caller has no branch of its own, so branch_id comes from the body.
func (a *ShiftAPI) CreateExpense(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	actor, ok := expenseActor(c)
	if !ok {
		return
	}
	var in shift.CreateExpenseRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if scope.BranchID != nil {
		in.BranchID = *scope.BranchID
	}
	// Overwrite, never trust: whichever isn't the caller is forced to zero.
	in.CreatedBy, in.CreatedByUserID = actor.StaffID, actor.UserID
	result, err := a.service.CreateExpense(c.Request.Context(), scope, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateExpense handles `PATCH /expenses/:id`. Only the staff member who
// logged the expense, a Manager (access_backoffice), or an Owner / Service
// Center acting directly may modify it - see shift.ExpenseActor.
func (a *ShiftAPI) UpdateExpense(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	actor, ok := expenseActor(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in shift.UpdateExpenseRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if scope.BranchID != nil && in.BranchID != nil {
		branchID := *scope.BranchID
		in.BranchID = &branchID
	}
	result, err := a.service.UpdateExpense(c.Request.Context(), scope, uint(idVal), actor, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteExpense handles `DELETE /expenses/:id`. Same authorization as
// UpdateExpense.
func (a *ShiftAPI) DeleteExpense(c *gin.Context) {
	scope, ok := shiftAccessScope(c)
	if !ok {
		return
	}
	actor, ok := expenseActor(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteExpense(c.Request.Context(), scope, uint(idVal), actor); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
