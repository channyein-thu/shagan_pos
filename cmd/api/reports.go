package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/reports"
)

type ReportsAPI struct {
	service reports.Interface
}

func NewReportsAPI(db *gorm.DB) *ReportsAPI {
	return &ReportsAPI{service: reports.NewService(reports.NewRepository(db))}
}

func (a *ReportsAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/reports/home-summary", a.GetHomeSummary)
	rg.GET("/reports/stock-overview", a.GetStockOverview)
	rg.GET("/reports/today", a.GetTodayReport)
	rg.GET("/reports/revenue-trend", a.GetRevenueTrend)
	rg.GET("/reports/sales-summary", a.GetSalesSummary)
	rg.GET("/reports/sales-trend", a.GetSalesTrend)
	rg.GET("/reports/payment-methods", a.GetPaymentMethodsReport)
	rg.GET("/reports/transactions", a.GetTransactionsReport)
	rg.GET("/reports/product-sales", a.GetProductSalesReport)
	rg.GET("/reports/top-products", a.GetTopProducts)
	rg.POST("/reports/export", a.ExportReport)
}

// GetHomeSummary handles `GET /reports/home-summary`.
func (a *ReportsAPI) GetHomeSummary(c *gin.Context) {
	result, err := a.service.GetHomeSummary(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetStockOverview handles `GET /reports/stock-overview`.
func (a *ReportsAPI) GetStockOverview(c *gin.Context) {
	result, err := a.service.GetStockOverview(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTodayReport handles `GET /reports/today`.
func (a *ReportsAPI) GetTodayReport(c *gin.Context) {
	result, err := a.service.GetTodayReport(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetRevenueTrend handles `GET /reports/revenue-trend`.
func (a *ReportsAPI) GetRevenueTrend(c *gin.Context) {
	result, err := a.service.GetRevenueTrend(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSalesSummary handles `GET /reports/sales-summary`.
func (a *ReportsAPI) GetSalesSummary(c *gin.Context) {
	result, err := a.service.GetSalesSummary(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSalesTrend handles `GET /reports/sales-trend`.
func (a *ReportsAPI) GetSalesTrend(c *gin.Context) {
	result, err := a.service.GetSalesTrend(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetPaymentMethodsReport handles `GET /reports/payment-methods`.
func (a *ReportsAPI) GetPaymentMethodsReport(c *gin.Context) {
	result, err := a.service.GetPaymentMethodsReport(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTransactionsReport handles `GET /reports/transactions`.
func (a *ReportsAPI) GetTransactionsReport(c *gin.Context) {
	result, err := a.service.GetTransactionsReport(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProductSalesReport handles `GET /reports/product-sales`.
func (a *ReportsAPI) GetProductSalesReport(c *gin.Context) {
	result, err := a.service.GetProductSalesReport(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTopProducts handles `GET /reports/top-products`.
func (a *ReportsAPI) GetTopProducts(c *gin.Context) {
	result, err := a.service.GetTopProducts(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ExportReport handles `POST /reports/export`. DEFER per the cut list
func (a *ReportsAPI) ExportReport(c *gin.Context) {
	result, err := a.service.ExportReport(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
