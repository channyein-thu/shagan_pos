package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
)

type AuditAPI struct {
	service audit.Interface
}

func NewAuditAPI(db *gorm.DB) *AuditAPI {
	return &AuditAPI{service: audit.NewService(audit.NewRepository(db))}
}

func (a *AuditAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/audit")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real audit listing endpoint.
func (a *AuditAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
