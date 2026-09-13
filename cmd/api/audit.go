package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
)

type AuditAPI struct {
	service audit.Interface
}

func NewAuditAPI(db *gorm.DB) *AuditAPI {
	return &AuditAPI{service: audit.NewService(audit.NewRepository(db))}
}

func (a *AuditAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/audit-log", a.ListAuditLog)
}

// ListAuditLog handles `GET /audit-log`. Read-only; written via mutation hooks, not a public POST
func (a *AuditAPI) ListAuditLog(c *gin.Context) {
	result, err := a.service.ListAuditLog(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
