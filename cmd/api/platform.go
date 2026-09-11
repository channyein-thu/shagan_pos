package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/platform"
)

type PlatformAPI struct {
	service platform.Interface
}

func NewPlatformAPI(db *gorm.DB) *PlatformAPI {
	return &PlatformAPI{service: platform.NewService(platform.NewRepository(db))}
}

func (a *PlatformAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/settings")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real platform listing endpoint.
func (a *PlatformAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
