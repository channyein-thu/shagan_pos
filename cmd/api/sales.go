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
	return &SalesAPI{service: newSalesService(db, jwtSecret), jwtSecret: jwtSecret}
}

// newSalesService wires the sales service. identity.Service (not just its
// repository) is passed as the StaffDirectory because it owns the PIN check -
// lockout counter included - that approves a custom discount.
func newSalesService(db *gorm.DB, jwtSecret []byte) *sales.Service {
	staff := identity.NewService(identity.NewRepository(db), audit.NewRepository(db), db, jwtSecret,
		identity.DefaultAccessTokenTTL, identity.DefaultRefreshTokenTTL, identity.DefaultStaffPINTokenTTL, identity.DefaultManagerPINTokenTTL)
	return sales.NewService(sales.NewRepository(db), inventory.NewRepository(db), catalog.NewRepository(db), audit.NewRepository(db), identity.NewRepository(db), returns.NewRepository(db), staff, db)
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
	// A custom discount is approved when it is added: the seller must be allowed
	// to ask (apply_manual_discount); who may approve is decided in the service.
	rg.POST("/sales/discount-approvals", middleware.RequireStaffPermission(a.jwtSecret, "apply_manual_discount"), a.CreateDiscountApproval)
	rg.DELETE("/sales/discount-approvals/:id", middleware.RequireStaffToken(a.jwtSecret), a.CancelDiscountApproval)
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
// attributed to whichever staff that token identifies. Every line is priced
// from its product; a custom discount is carried by a discount_approval_id
// the same seller got when the discount was added (POST
// /sales/discount-approvals) - payment never asks for a PIN, and
// X-Manager-Approval-Token no longer has anything to do with discounts.
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
	actor := sales.SaleActor{StaffID: staffID}
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

// CreateDiscountApproval handles `POST /sales/discount-approvals`. The seller
// is the X-Staff-Token's staff member (needs apply_manual_discount); the
// approver is either another staff member entering their PIN (approver_staff_id
// + pin) or, with neither, the seller approving their own (needs
// approve_manual_discount and approve_own_discount). Returns the prices the
// backend will charge. Approved when the discount is ADDED, not at payment.
func (a *SalesAPI) CreateDiscountApproval(c *gin.Context) {
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
	posUserID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	var in sales.CreateDiscountApprovalRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	seller := sales.DiscountSeller{StaffID: staffID, PosUserID: posUserID, Permissions: middleware.StaffPermissions(c)}
	result, err := a.service.CreateDiscountApproval(c.Request.Context(), orgID, branchID, seller, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// CancelDiscountApproval handles `DELETE /sales/discount-approvals/:id`: the
// till removed the discount before paying. 409 if it was already used.
func (a *SalesAPI) CancelDiscountApproval(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	staffID, ok := requireStaffID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.CancelDiscountApproval(c.Request.Context(), orgID, staffID, id); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
