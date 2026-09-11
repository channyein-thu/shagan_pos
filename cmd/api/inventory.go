package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/inventory"
)

type InventoryAPI struct {
	service inventory.Interface
}

func NewInventoryAPI(db *gorm.DB) *InventoryAPI {
	return &InventoryAPI{service: inventory.NewService(inventory.NewRepository(db))}
}

func (a *InventoryAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/inventory")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real inventory listing endpoint.
func (a *InventoryAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
