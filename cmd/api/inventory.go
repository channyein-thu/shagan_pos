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
)

type InventoryAPI struct {
	service inventory.Interface
}

func NewInventoryAPI(db *gorm.DB) *InventoryAPI {
	return &InventoryAPI{
		service: inventory.NewService(inventory.NewRepository(db), identity.NewRepository(db), catalog.NewRepository(db), db),
	}
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

// ListStockLevels handles `GET /stock-levels`. Restricted to the caller's
// own branch when the caller's token carries one (a pos device) -
// org-wide for owner/service_center, same reasoning as
// catalog.ListProducts. Optional ?branch_id= (owner/service_center only -
// a pos-device token's own branch always wins) and ?product_id= further
// narrow the results.
func (a *InventoryAPI) ListStockLevels(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	branchID, productID, ok := branchAndProductFilters(c)
	if !ok {
		return
	}

	result, err := a.service.ListStockLevels(c.Request.Context(), orgID, branchID, productID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListLowStock handles `GET /inventory/low-stock`. Same scoping as
// ListStockLevels.
func (a *InventoryAPI) ListLowStock(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, _, ok := branchAndProductFilters(c)
	if !ok {
		return
	}
	result, err := a.service.ListLowStock(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListInventoryLedger handles `GET /inventory/ledger`. Read-only - never
// written directly by a client. Restricted to the caller's own branch when
// the caller's token carries one (a pos device) - org-wide for
// owner/service_center, same reasoning as ListStockLevels; unlike
// ListStockLevels, an owner-supplied ?branch_id= needs no separate
// ownership check (see Service.ListInventoryLedger's doc). Optional
// ?product_id= further narrows the results.
func (a *InventoryAPI) ListInventoryLedger(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	branchID, productID, ok := branchAndProductFilters(c)
	if !ok {
		return
	}

	result, err := a.service.ListInventoryLedger(c.Request.Context(), orgID, branchID, productID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// branchAndProductFilters reads the optional ?branch_id=/?product_id=
// query params shared by ListStockLevels and ListInventoryLedger - a
// pos-device token's own branch always wins over any client-supplied
// branch_id, same reasoning as catalog.ListProducts. Writes a 400 itself
// and returns ok=false on a malformed value.
func branchAndProductFilters(c *gin.Context) (branchID *uint, productID *uint, ok bool) {
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = &bID
	} else if v := c.Query("branch_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid branch_id"))
			return nil, nil, false
		}
		bID := uint(id)
		branchID = &bID
	}

	if v := c.Query("product_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid product_id"))
			return nil, nil, false
		}
		pID := uint(id)
		productID = &pID
	}

	return branchID, productID, true
}

// CreateStockAdjustment handles `POST /inventory/adjustments`. Writes a
// ledger row as a side effect. actor_id isn't accepted from the client -
// it's the authenticated caller's own user ID.
func (a *InventoryAPI) CreateStockAdjustment(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	var in inventory.CreateStockAdjustmentRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateStockAdjustment(c.Request.Context(), orgID, actorID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListStockTransfers handles `GET /stock-transfers`. Same scoping as
// ListStockLevels (branch_id optionally narrows to transfers touching that
// branch, either as sender or receiver).
func (a *InventoryAPI) ListStockTransfers(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, _, ok := branchAndProductFilters(c)
	if !ok {
		return
	}
	result, err := a.service.ListStockTransfers(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateStockTransfer handles `POST /stock-transfers`. Also writes
// stock_transfers_items. actor_id isn't accepted from the client - it's the
// authenticated caller's own user ID.
func (a *InventoryAPI) CreateStockTransfer(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	var in inventory.CreateStockTransferRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateStockTransfer(c.Request.Context(), orgID, actorID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateStockTransfer handles `PATCH /stock-transfers/:id`. Status
// lifecycle: pending -> in_transit -> completed, or -> cancelled. Moving to
// completed is the transition that actually decrements/increments stock -
// see Service.UpdateStockTransfer.
func (a *InventoryAPI) UpdateStockTransfer(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
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
	result, err := a.service.UpdateStockTransfer(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
