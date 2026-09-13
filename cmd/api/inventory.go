package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/inventory"
)

type InventoryAPI struct {
	service inventory.Interface
}

func NewInventoryAPI(db *gorm.DB) *InventoryAPI {
	return &InventoryAPI{service: inventory.NewService(inventory.NewRepository(db))}
}

func (a *InventoryAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/stock-levels", a.ListStockLevels)
	rg.GET("/inventory/low-stock", a.ListLowStock)
	rg.GET("/inventory/ledger", a.ListInventoryLedger)
	rg.POST("/inventory/adjustments", a.CreateStockAdjustment)
	rg.GET("/stock-transfers", a.ListStockTransfers)
	rg.POST("/stock-transfers", a.CreateStockTransfer)
	rg.PATCH("/stock-transfers/:id", a.UpdateStockTransfer)
}

// ListStockLevels handles `GET /stock-levels`. Filter by product/branch
func (a *InventoryAPI) ListStockLevels(c *gin.Context) {
	result, err := a.service.ListStockLevels(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListLowStock handles `GET /inventory/low-stock`.
func (a *InventoryAPI) ListLowStock(c *gin.Context) {
	result, err := a.service.ListLowStock(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListInventoryLedger handles `GET /inventory/ledger`. Read-only - never written directly by a client
func (a *InventoryAPI) ListInventoryLedger(c *gin.Context) {
	result, err := a.service.ListInventoryLedger(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateStockAdjustment handles `POST /inventory/adjustments`. Writes a ledger row as a side effect
func (a *InventoryAPI) CreateStockAdjustment(c *gin.Context) {
	var in inventory.CreateStockAdjustmentRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateStockAdjustment(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListStockTransfers handles `GET /stock-transfers`.
func (a *InventoryAPI) ListStockTransfers(c *gin.Context) {
	result, err := a.service.ListStockTransfers(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateStockTransfer handles `POST /stock-transfers`. Also writes stock_transfers_items
func (a *InventoryAPI) CreateStockTransfer(c *gin.Context) {
	var in inventory.CreateStockTransferRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateStockTransfer(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateStockTransfer handles `PATCH /stock-transfers/:id`. Status lifecycle: pending -> in-transit -> received
func (a *InventoryAPI) UpdateStockTransfer(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in inventory.UpdateStockTransferRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateStockTransfer(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
