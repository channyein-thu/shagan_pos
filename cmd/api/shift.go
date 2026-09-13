package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/shift"
)

type ShiftAPI struct {
	service shift.Interface
}

func NewShiftAPI(db *gorm.DB) *ShiftAPI {
	return &ShiftAPI{service: shift.NewService(shift.NewRepository(db))}
}

func (a *ShiftAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/shifts", a.OpenShift)
	rg.GET("/shifts/current", a.GetCurrentShift)
	rg.GET("/shifts/:id", a.GetShift)
	rg.POST("/shifts/:id/close", a.CloseShift)
	rg.GET("/shifts/:id/summary", a.GetShiftSummary)
	rg.GET("/shifts/:id/reconciliations", a.ListShiftReconciliations)
	rg.POST("/drawer-events", a.CreateDrawerEvent)
	rg.GET("/drawer-events", a.ListDrawerEvents)
	rg.GET("/expenses", a.ListExpenses)
	rg.POST("/expenses", a.CreateExpense)
	rg.PATCH("/expenses/:id", a.UpdateExpense)
	rg.DELETE("/expenses/:id", a.DeleteExpense)
}

// OpenShift handles `POST /shifts`. Open shift with opening float
func (a *ShiftAPI) OpenShift(c *gin.Context) {
	var in shift.Shift
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.OpenShift(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetCurrentShift handles `GET /shifts/current`.
func (a *ShiftAPI) GetCurrentShift(c *gin.Context) {
	result, err := a.service.GetCurrentShift(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetShift handles `GET /shifts/:id`.
func (a *ShiftAPI) GetShift(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetShift(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CloseShift handles `POST /shifts/:id/close`. Writes reconciliation row(s) as a side effect
func (a *ShiftAPI) CloseShift(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.CloseShift(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetShiftSummary handles `GET /shifts/:id/summary`. Printable summary
func (a *ShiftAPI) GetShiftSummary(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetShiftSummary(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListShiftReconciliations handles `GET /shifts/:id/reconciliations`. Per-method breakdown
func (a *ShiftAPI) ListShiftReconciliations(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ListShiftReconciliations(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateDrawerEvent handles `POST /drawer-events`. Cash drawer opened without a sale
func (a *ShiftAPI) CreateDrawerEvent(c *gin.Context) {
	var in shift.DrawerEvent
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateDrawerEvent(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListDrawerEvents handles `GET /drawer-events`.
func (a *ShiftAPI) ListDrawerEvents(c *gin.Context) {
	result, err := a.service.ListDrawerEvents(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListExpenses handles `GET /expenses`.
func (a *ShiftAPI) ListExpenses(c *gin.Context) {
	result, err := a.service.ListExpenses(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateExpense handles `POST /expenses`.
func (a *ShiftAPI) CreateExpense(c *gin.Context) {
	var in shift.Expense
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateExpense(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateExpense handles `PATCH /expenses/:id`.
func (a *ShiftAPI) UpdateExpense(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in shift.Expense
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateExpense(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteExpense handles `DELETE /expenses/:id`.
func (a *ShiftAPI) DeleteExpense(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteExpense(c.Request.Context(), uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
