package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/procurement"
)

type ProcurementAPI struct {
	service   procurement.Interface
	jwtSecret []byte
}

func NewProcurementAPI(db *gorm.DB, jwtSecret []byte) *ProcurementAPI {
	return &ProcurementAPI{
		service:   procurement.NewService(procurement.NewRepository(db), identity.NewRepository(db), catalog.NewRepository(db), inventory.NewRepository(db), db),
		jwtSecret: jwtSecret,
	}
}

func (a *ProcurementAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// Supplier and purchase-order writes need Back Office. Suppliers are
	// org-wide and shared; purchase orders are branch-scoped, so a manager
	// at the till is restricted to their own branch's orders in the handlers.
	backOffice := middleware.RequireBackOffice(a.jwtSecret)
	rg.GET("/suppliers", a.ListSuppliers)
	rg.POST("/suppliers", backOffice, a.CreateSupplier)
	rg.PATCH("/suppliers/:id", backOffice, a.UpdateSupplier)
	rg.DELETE("/suppliers/:id", backOffice, a.DeleteSupplier)
	rg.GET("/purchase-orders", a.ListPurchaseOrders)
	rg.POST("/purchase-orders", backOffice, a.CreatePurchaseOrder)
	rg.GET("/purchase-orders/:id", a.GetPurchaseOrder)
	rg.PATCH("/purchase-orders/:id", backOffice, a.UpdatePurchaseOrder)
	rg.POST("/purchase-orders/:id/receipts", backOffice, a.CreateGoodsReceipt)
}

// ListSuppliers handles `GET /suppliers`.
func (a *ProcurementAPI) ListSuppliers(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListSuppliers(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateSupplier handles `POST /suppliers`.
func (a *ProcurementAPI) CreateSupplier(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var in procurement.CreateSupplierRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateSupplier(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateSupplier handles `PATCH /suppliers/:id`.
func (a *ProcurementAPI) UpdateSupplier(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in procurement.UpdateSupplierRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateSupplier(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteSupplier handles `DELETE /suppliers/:id`. Hard delete, blocked if
// any purchase order still references the supplier.
func (a *ProcurementAPI) DeleteSupplier(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteSupplier(c.Request.Context(), orgID, uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListPurchaseOrders handles `GET /purchase-orders`.
func (a *ProcurementAPI) ListPurchaseOrders(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListPurchaseOrders(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	// A POS-device token is branch-bound: only its own branch's orders.
	if ownBranchID, ok := middleware.BranchIDFromContext(c); ok {
		own := make([]procurement.PurchaseOrder, 0, len(result))
		for _, po := range result {
			if po.BranchID == ownBranchID {
				own = append(own, po)
			}
		}
		result = own
	}
	c.JSON(http.StatusOK, result)
}

// CreatePurchaseOrder handles `POST /purchase-orders`. Also writes purchase_order_items
func (a *ProcurementAPI) CreatePurchaseOrder(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	createdBy, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	var in procurement.CreatePurchaseOrderRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	// A manager at the till may only raise orders for their own branch.
	branchID, ok := scopedBranchID(c, in.BranchID)
	if !ok {
		return
	}
	in.BranchID = branchID
	result, err := a.service.CreatePurchaseOrder(c.Request.Context(), orgID, createdBy, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetPurchaseOrder handles `GET /purchase-orders/:id`.
func (a *ProcurementAPI) GetPurchaseOrder(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetPurchaseOrder(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	if ownBranchID, ok := middleware.BranchIDFromContext(c); ok && result.BranchID != ownBranchID {
		common.HandleError(c, common.NotFoundError("purchase order not found"))
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdatePurchaseOrder handles `PATCH /purchase-orders/:id`. Status transitions
func (a *ProcurementAPI) UpdatePurchaseOrder(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in procurement.UpdatePurchaseOrderRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if !a.requirePurchaseOrderInOwnBranch(c, orgID, uint(idVal)) {
		return
	}
	result, err := a.service.UpdatePurchaseOrder(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateGoodsReceipt handles `POST /purchase-orders/:id/receipts`. Posting increases stock + writes ledger
func (a *ProcurementAPI) CreateGoodsReceipt(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	receivedBy, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in procurement.CreateGoodsReceiptRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if !a.requirePurchaseOrderInOwnBranch(c, orgID, uint(idVal)) {
		return
	}
	result, err := a.service.CreateGoodsReceipt(c.Request.Context(), orgID, uint(idVal), receivedBy, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// requirePurchaseOrderInOwnBranch restricts a branch-bound (POS-device)
// token to purchase orders of its own branch; owner/service_center tokens
// carry no branch and pass straight through. A foreign branch's order is
// reported as not found, not forbidden, so a till can't probe other
// branches' order ids. Writes the error response itself and returns false
// when the request must stop.
func (a *ProcurementAPI) requirePurchaseOrderInOwnBranch(c *gin.Context, orgID, id uint) bool {
	ownBranchID, ok := middleware.BranchIDFromContext(c)
	if !ok {
		return true
	}
	po, err := a.service.GetPurchaseOrder(c.Request.Context(), orgID, id)
	if err != nil {
		common.HandleError(c, err)
		return false
	}
	if po.BranchID != ownBranchID {
		common.HandleError(c, common.NotFoundError("purchase order not found"))
		return false
	}
	return true
}
