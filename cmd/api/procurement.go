package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/procurement"
)

type ProcurementAPI struct {
	service procurement.Interface
}

func NewProcurementAPI(db *gorm.DB) *ProcurementAPI {
	return &ProcurementAPI{service: procurement.NewService(procurement.NewRepository(db))}
}

func (a *ProcurementAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/suppliers", a.ListSuppliers)
	rg.POST("/suppliers", a.CreateSupplier)
	rg.PATCH("/suppliers/:id", a.UpdateSupplier)
	rg.DELETE("/suppliers/:id", a.DeleteSupplier)
	rg.GET("/purchase-orders", a.ListPurchaseOrders)
	rg.POST("/purchase-orders", a.CreatePurchaseOrder)
	rg.GET("/purchase-orders/:id", a.GetPurchaseOrder)
	rg.PATCH("/purchase-orders/:id", a.UpdatePurchaseOrder)
	rg.POST("/purchase-orders/:id/receipts", a.CreateGoodsReceipt)
}

// ListSuppliers handles `GET /suppliers`.
func (a *ProcurementAPI) ListSuppliers(c *gin.Context) {
	result, err := a.service.ListSuppliers(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateSupplier handles `POST /suppliers`.
func (a *ProcurementAPI) CreateSupplier(c *gin.Context) {
	var in procurement.Supplier
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateSupplier(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateSupplier handles `PATCH /suppliers/:id`.
func (a *ProcurementAPI) UpdateSupplier(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in procurement.Supplier
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateSupplier(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteSupplier handles `DELETE /suppliers/:id`.
func (a *ProcurementAPI) DeleteSupplier(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteSupplier(c.Request.Context(), uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListPurchaseOrders handles `GET /purchase-orders`.
func (a *ProcurementAPI) ListPurchaseOrders(c *gin.Context) {
	result, err := a.service.ListPurchaseOrders(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreatePurchaseOrder handles `POST /purchase-orders`. Also writes purchase_order_items
func (a *ProcurementAPI) CreatePurchaseOrder(c *gin.Context) {
	var in procurement.PurchaseOrder
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreatePurchaseOrder(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetPurchaseOrder handles `GET /purchase-orders/:id`.
func (a *ProcurementAPI) GetPurchaseOrder(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetPurchaseOrder(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdatePurchaseOrder handles `PATCH /purchase-orders/:id`. Status transitions
func (a *ProcurementAPI) UpdatePurchaseOrder(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in procurement.PurchaseOrder
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdatePurchaseOrder(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateGoodsReceipt handles `POST /purchase-orders/:id/receipts`. Posting increases stock + writes ledger
func (a *ProcurementAPI) CreateGoodsReceipt(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in procurement.GoodsReceipt
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateGoodsReceipt(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
