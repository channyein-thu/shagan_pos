package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/datasync"
)

type SyncAPI struct {
	service datasync.Interface
}

func NewSyncAPI(db *gorm.DB) *SyncAPI {
	return &SyncAPI{service: datasync.NewService(datasync.NewRepository(db))}
}

func (a *SyncAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/sync")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real datasync listing endpoint.
func (a *SyncAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
