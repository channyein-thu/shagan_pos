package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/reports"
)

type ReportsAPI struct {
	service   reports.Interface
	jwtSecret []byte
}

func NewReportsAPI(db *gorm.DB, jwtSecret []byte) *ReportsAPI {
	return &ReportsAPI{service: reports.NewService(reports.NewRepository(db), identity.NewRepository(db)), jwtSecret: jwtSecret}
}

func (a *ReportsAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// view_reports only restricts a caller using Back Office (a staff token
	// with access_backoffice); an Owner / Service Center always passes.
	viewReports := middleware.RequireViewPermission(a.jwtSecret, "view_reports")
	rg.GET("/reports/home-summary", viewReports, a.GetHomeSummary)
	rg.GET("/reports/stock-overview", viewReports, a.GetStockOverview)
	rg.GET("/reports/today", viewReports, a.GetTodayReport)
	rg.GET("/reports/revenue-trend", viewReports, a.GetRevenueTrend)
	rg.GET("/reports/sales-summary", viewReports, a.GetSalesSummary)
	rg.GET("/reports/sales-trend", viewReports, a.GetSalesTrend)
	rg.GET("/reports/payment-methods", viewReports, a.GetPaymentMethodsReport)
	rg.GET("/reports/transactions", viewReports, a.GetTransactionsReport)
	rg.GET("/reports/product-sales", viewReports, a.GetProductSalesReport)
	rg.GET("/reports/top-products", viewReports, a.GetTopProducts)
	rg.GET("/reports/profit-loss", viewReports, a.GetProfitAndLoss)
	rg.POST("/reports/export", viewReports, a.ExportReport)
}

// reportBranchID reads the shared ?branch_id= query param - a
// branch-scoped token's own branch always wins, same convention as
// inventory's branchAndProductFilters.
func reportBranchID(c *gin.Context) (*uint, bool) {
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		return &bID, true
	}
	if v := c.Query("branch_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid branch_id"))
			return nil, false
		}
		bID := uint(id)
		return &bID, true
	}
	return nil, true
}

// reportDateRange reads the shared optional ?from=/?to= query params
// (YYYY-MM-DD) - nil means "let the service apply its own default".
func reportDateRange(c *gin.Context) (from, to *time.Time, ok bool) {
	if v := c.Query("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid from date, expected YYYY-MM-DD"))
			return nil, nil, false
		}
		from = &t
	}
	if v := c.Query("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid to date, expected YYYY-MM-DD"))
			return nil, nil, false
		}
		to = &t
	}
	return from, to, true
}

// GetHomeSummary handles `GET /reports/home-summary`.
func (a *ReportsAPI) GetHomeSummary(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.GetHomeSummary(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetStockOverview handles `GET /reports/stock-overview`.
func (a *ReportsAPI) GetStockOverview(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.GetStockOverview(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTodayReport handles `GET /reports/today`.
func (a *ReportsAPI) GetTodayReport(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.GetTodayReport(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetRevenueTrend handles `GET /reports/revenue-trend`.
func (a *ReportsAPI) GetRevenueTrend(c *gin.Context) {
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
	granularity := reports.Granularity(c.Query("granularity"))
	result, err := a.service.GetRevenueTrend(c.Request.Context(), orgID, branchID, from, to, granularity)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSalesSummary handles `GET /reports/sales-summary`.
func (a *ReportsAPI) GetSalesSummary(c *gin.Context) {
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
	result, err := a.service.GetSalesSummary(c.Request.Context(), orgID, branchID, from, to)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSalesTrend handles `GET /reports/sales-trend`.
func (a *ReportsAPI) GetSalesTrend(c *gin.Context) {
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
	granularity := reports.Granularity(c.Query("granularity"))
	result, err := a.service.GetSalesTrend(c.Request.Context(), orgID, branchID, from, to, granularity)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetPaymentMethodsReport handles `GET /reports/payment-methods`.
func (a *ReportsAPI) GetPaymentMethodsReport(c *gin.Context) {
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
	result, err := a.service.GetPaymentMethodsReport(c.Request.Context(), orgID, branchID, from, to)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTransactionsReport handles `GET /reports/transactions`.
func (a *ReportsAPI) GetTransactionsReport(c *gin.Context) {
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
	result, err := a.service.GetTransactionsReport(c.Request.Context(), orgID, branchID, from, to, page, pageSize)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProductSalesReport handles `GET /reports/product-sales`.
func (a *ReportsAPI) GetProductSalesReport(c *gin.Context) {
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
	var categoryID *uint
	if v := c.Query("category_id"); v != "" {
		id, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid category_id"))
			return
		}
		cID := uint(id)
		categoryID = &cID
	}
	result, err := a.service.GetProductSalesReport(c.Request.Context(), orgID, branchID, from, to, categoryID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTopProducts handles `GET /reports/top-products`.
func (a *ReportsAPI) GetTopProducts(c *gin.Context) {
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
	var limit *int
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid limit"))
			return
		}
		limit = &n
	}
	result, err := a.service.GetTopProducts(c.Request.Context(), orgID, branchID, from, to, limit)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProfitAndLoss handles `GET /reports/profit-loss`.
func (a *ReportsAPI) GetProfitAndLoss(c *gin.Context) {
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
	result, err := a.service.GetProfitAndLoss(c.Request.Context(), orgID, branchID, from, to)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	// COGS and profit are what view_item_cost protects.
	if !middleware.CallerHasPermission(c, "view_item_cost") {
		c.JSON(http.StatusOK, result.WithoutCost())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ExportReport handles `POST /reports/export`. Deferred - see
// reports.Interface.ExportReport's doc.
func (a *ReportsAPI) ExportReport(c *gin.Context) {
	result, err := a.service.ExportReport(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
