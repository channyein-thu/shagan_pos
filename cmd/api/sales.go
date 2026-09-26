package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/sales"
)

type SalesAPI struct {
	service sales.Interface
}

func NewSalesAPI(db *gorm.DB) *SalesAPI {
	return &SalesAPI{service: sales.NewService(sales.NewRepository(db), db)}
}

// requireBranchID reads the calling pos-device's branch from its access
// token - a sale can only ever be rung up for the device's own branch, never
// a client-supplied one.
func requireBranchID(c *gin.Context) (uint, bool) {
	branchID, ok := middleware.BranchIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("this endpoint requires a branch-bound pos device token"))
	}
	return branchID, ok
}

func (a *SalesAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/sales", a.CreateSale)
	rg.GET("/sales", a.ListSales)
	rg.GET("/sales/:id", a.GetSale)
	rg.GET("/sales/:id/receipt", a.GetSaleReceipt)
	rg.POST("/sales/:id/reprint", a.ReprintSale)
	rg.POST("/held-sales", a.CreateHeldSale)
	rg.GET("/held-sales", a.ListHeldSales)
	rg.DELETE("/held-sales/:id", a.ResumeHeldSale)
}

// CreateSale handles `POST /sales`. Idempotent (ID is client-generated),
// transactional. Stock decrement/ledger writes are deliberately deferred
// until the Inventory domain is implemented - see the sales_service_impl.go
// doc comment on CreateSale.
func (a *SalesAPI) CreateSale(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := requireBranchID(c)
	if !ok {
		return
	}
	var in sales.CreateSaleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateSale(c.Request.Context(), orgID, branchID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListSales handles `GET /sales`.
func (a *SalesAPI) ListSales(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListSales(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSale handles `GET /sales/:id`.
func (a *SalesAPI) GetSale(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetSale(c.Request.Context(), orgID, id)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSaleReceipt handles `GET /sales/:id/receipt`. Structured receipt data
// (sale + items + payments) - not printer-specific ESC/POS byte formatting,
// which belongs to Platform's printer integration, not here.
func (a *SalesAPI) GetSaleReceipt(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetSaleReceipt(c.Request.Context(), orgID, id)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ReprintSale handles `POST /sales/:id/reprint`.
func (a *SalesAPI) ReprintSale(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ReprintSale(c.Request.Context(), orgID, id)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateHeldSale handles `POST /held-sales`.
func (a *SalesAPI) CreateHeldSale(c *gin.Context) {
	var in sales.CreateHeldSaleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateHeldSale(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListHeldSales handles `GET /held-sales`.
func (a *SalesAPI) ListHeldSales(c *gin.Context) {
	result, err := a.service.ListHeldSales(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResumeHeldSale handles `DELETE /held-sales/:id`. Resume - atomic delete-and-restore
func (a *SalesAPI) ResumeHeldSale(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ResumeHeldSale(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
