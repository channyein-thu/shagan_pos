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

// ListAuditLog handles `GET /audit-log`. Read-only; written via mutation
// hooks elsewhere, not a public POST. Restricted to the caller's own branch
// when the token carries one - org-wide otherwise, same reasoning as
// inventory.ListStockLevels. Optional ?branch_id= (owner/service_center
// only - a pos-device token's own branch always wins).
func (a *AuditAPI) ListAuditLog(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListAuditLog(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
