package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/returns"
	"shagan_pos/internal/sales"
)

type SalesAPI struct {
	service   sales.Interface
	jwtSecret []byte
}

func NewSalesAPI(db *gorm.DB, jwtSecret []byte) *SalesAPI {
	return &SalesAPI{service: sales.NewService(sales.NewRepository(db), inventory.NewRepository(db), catalog.NewRepository(db), audit.NewRepository(db), identity.NewRepository(db), returns.NewRepository(db), db), jwtSecret: jwtSecret}
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
	// Till actions need their own permission on the X-Staff-Token: ringing up
	// a sale (accept_payments), holding/resuming (hold_resume_sales),
	// reprinting (reprint_receipt - an Owner / Service Center may too). The
	// sales history is read by the till as well, so view_sales only restricts
	// a caller using Back Office; a cashier without view_all_receipts is shown
	// only their own sales in the list (see ListSales).
	acceptPayments := middleware.RequireStaffPermission(a.jwtSecret, "accept_payments")
	holdResume := middleware.RequireStaffPermission(a.jwtSecret, "hold_resume_sales")
	reprint := middleware.RequireStaffPermissionOrOrgAdmin(a.jwtSecret, "reprint_receipt")
	viewSales := middleware.RequireViewPermission(a.jwtSecret, "view_sales")
	rg.POST("/sales", acceptPayments, a.CreateSale)
	rg.GET("/sales", viewSales, a.ListSales)
	rg.GET("/sales/:id", viewSales, a.GetSale)
	rg.GET("/sales/:id/receipt", viewSales, a.GetSaleReceipt)
	rg.POST("/sales/:id/reprint", reprint, a.ReprintSale)
	rg.POST("/held-sales", holdResume, a.CreateHeldSale)
	rg.GET("/held-sales", a.ListHeldSales)
	rg.DELETE("/held-sales/:id", holdResume, a.ResumeHeldSale)
}

// CreateSale handles `POST /sales`. Idempotent by the client-generated ID: a
// retry of a sale this same device already recorded returns it again with
// 200 instead of 201, a duplicate id from a different branch/device is 409.
// Transactional. Stock decrement/ledger writes are deliberately deferred
// until the Inventory domain is implemented - see the sales_service_impl.go
// doc comment on CreateSale. Requires X-Staff-Token - the sale is always
// attributed to whichever staff that token identifies, and a discount on
// any item is rejected (403) unless that staff's role grants
// apply_manual_discount, OR an optional X-Manager-Approval-Token is present
// granting it instead (see middleware.ManagerApproved) - a cashier without
// the permission gets a manager to approve just this one sale.
func (a *SalesAPI) CreateSale(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := requireBranchID(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	var in sales.CreateSaleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	in.StaffID = staffID
	canApplyManualDiscount := middleware.StaffHasPermission(c, "apply_manual_discount") ||
		middleware.ManagerApproved(c, a.jwtSecret, "apply_manual_discount")
	actor := sales.SaleActor{StaffID: staffID, CanApplyManualDiscount: canApplyManualDiscount}
	result, _, err := a.service.CreateSale(c.Request.Context(), orgID, branchID, actor, in, false)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	if result.Replayed {
		// A retry of an already-recorded sale: same body back, nothing new
		// was created.
		c.JSON(http.StatusOK, result)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListSales handles `GET /sales`. Optional ?branch_id= (owner/service_center
// only - a pos-device token's own branch always wins, same as the reports),
// ?from=/?to= (YYYY-MM-DD, inclusive), ?page= and ?page_size= (default 20,
// max 100). Each row carries payment_methods.
func (a *SalesAPI) ListSales(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	from, to, ok := reportDateRange(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	// A cashier whose role lacks view_all_receipts sees only their own sales.
	// This is a till rule: a caller using Back Office (access_backoffice) is
	// governed by view_sales instead and sees them all, and an Owner has no
	// staff token at all.
	var staffID *uint
	if id, ok := middleware.StaffIDFromContext(c); ok &&
		!middleware.StaffHasPermission(c, "access_backoffice") &&
		!middleware.StaffHasPermission(c, "view_all_receipts") {
		staffID = &id
	}
	result, err := a.service.ListSales(c.Request.Context(), orgID, branchID, staffID, from, to, page, pageSize)
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

// CreateHeldSale handles `POST /held-sales`. Parks the calling staff's
// current cart - requires X-Staff-Token (attributed to whichever staff that
// token identifies, never a client-supplied staff_id).
func (a *SalesAPI) CreateHeldSale(c *gin.Context) {
	branchID, ok := requireBranchID(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	var in sales.CreateHeldSaleRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateHeldSale(c.Request.Context(), branchID, staffID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListHeldSales handles `GET /held-sales`. Branch-scoped, not staff-scoped -
// any staff at the branch sees every held sale there, not just their own.
func (a *SalesAPI) ListHeldSales(c *gin.Context) {
	branchID, ok := requireBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListHeldSales(c.Request.Context(), branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResumeHeldSale handles `DELETE /held-sales/:id`. Resume - atomic
// delete-and-restore. Branch-scoped, not staff-scoped - any staff at the
// branch can resume a held sale, not just whoever parked it.
func (a *SalesAPI) ResumeHeldSale(c *gin.Context) {
	branchID, ok := requireBranchID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ResumeHeldSale(c.Request.Context(), branchID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
