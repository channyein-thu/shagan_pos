package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/customer"
)

type CustomerAPI struct {
	service customer.Interface
}

func NewCustomerAPI(db *gorm.DB) *CustomerAPI {
	return &CustomerAPI{service: customer.NewService(customer.NewRepository(db))}
}

func (a *CustomerAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/customers", a.ListCustomers)
	rg.POST("/customers", a.CreateCustomer)
	rg.GET("/customers/:id", a.GetCustomer)
	rg.GET("/customers/:id/consents", a.ListCustomerConsents)
	rg.POST("/customers/:id/consents", a.CreateCustomerConsent)
}

// ListCustomers handles `GET /customers`.
func (a *CustomerAPI) ListCustomers(c *gin.Context) {
	result, err := a.service.ListCustomers(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCustomer handles `POST /customers`. Inline creation from the POS cart
func (a *CustomerAPI) CreateCustomer(c *gin.Context) {
	var in customer.Customer
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCustomer(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetCustomer handles `GET /customers/:id`. Detail + purchase history
func (a *CustomerAPI) GetCustomer(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetCustomer(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListCustomerConsents handles `GET /customers/:id/consents`.
func (a *CustomerAPI) ListCustomerConsents(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ListCustomerConsents(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCustomerConsent handles `POST /customers/:id/consents`. Also updates the cached customers.consent_status
func (a *CustomerAPI) CreateCustomerConsent(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in customer.CustomerConsent
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCustomerConsent(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
