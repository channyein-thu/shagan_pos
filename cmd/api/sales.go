package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/sales"
)

type SalesAPI struct {
	service sales.Interface
}

func NewSalesAPI(db *gorm.DB) *SalesAPI {
	return &SalesAPI{service: sales.NewService(sales.NewRepository(db))}
}

func (a *SalesAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/sales")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real sales listing endpoint.
func (a *SalesAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
