package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
)

type CatalogAPI struct {
	service catalog.Interface
}

func NewCatalogAPI(db *gorm.DB) *CatalogAPI {
	return &CatalogAPI{service: catalog.NewService(catalog.NewRepository(db))}
}

func (a *CatalogAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/catalog")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real catalog listing endpoint.
func (a *CatalogAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
