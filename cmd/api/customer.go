package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/customer"
	"shagan_pos/internal/middleware"
)

type CustomerAPI struct {
	service   customer.Interface
	jwtSecret []byte
}

func NewCustomerAPI(db *gorm.DB, jwtSecret []byte) *CustomerAPI {
	return &CustomerAPI{service: customer.NewService(customer.NewRepository(db), db), jwtSecret: jwtSecret}
}

func (a *CustomerAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// Adding a customer (and recording their consent as part of it) is a till
	// action: it needs a staff token with add_customers - or manage_customers,
	// which includes it - unless an Owner / Service Center acts directly.
	// Reading the customer list only needs view_customers from a caller using
	// Back Office; the till's own customer lookup is unchanged.
	addCustomers := middleware.RequireStaffPermissionOrOrgAdmin(a.jwtSecret, "add_customers", "manage_customers")
	viewCustomers := middleware.RequireViewPermission(a.jwtSecret, "view_customers")
	rg.GET("/customers", viewCustomers, a.ListCustomers)
	rg.POST("/customers", addCustomers, a.CreateCustomer)
	rg.GET("/customers/:id", viewCustomers, a.GetCustomer)
	rg.GET("/customers/:id/consents", viewCustomers, a.ListCustomerConsents)
	rg.POST("/customers/:id/consents", addCustomers, a.CreateCustomerConsent)
}

// ListCustomers handles `GET /customers`. search is an optional query param
// matching a partial name OR phone in one field.
func (a *CustomerAPI) ListCustomers(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	search := c.Query("search")
	result, err := a.service.ListCustomers(c.Request.Context(), orgID, search)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCustomer handles `POST /customers`. Inline creation from the POS cart
func (a *CustomerAPI) CreateCustomer(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var in customer.CreateCustomerRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCustomer(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// GetCustomer handles `GET /customers/:id`. Detail only for now - purchase
// history needs Sales, which doesn't exist yet.
func (a *CustomerAPI) GetCustomer(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetCustomer(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListCustomerConsents handles `GET /customers/:id/consents`.
func (a *CustomerAPI) ListCustomerConsents(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ListCustomerConsents(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCustomerConsent handles `POST /customers/:id/consents`. Also updates the cached customers.consent_status
func (a *CustomerAPI) CreateCustomerConsent(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in customer.CreateCustomerConsentRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCustomerConsent(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}
