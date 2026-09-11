package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/customer"
)

type CustomerAPI struct {
	service customer.Interface
}

func NewCustomerAPI(db *gorm.DB) *CustomerAPI {
	return &CustomerAPI{service: customer.NewService(customer.NewRepository(db))}
}

func (a *CustomerAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/customers")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real customer listing endpoint.
func (a *CustomerAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
