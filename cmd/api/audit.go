package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/middleware"
)

type AuditAPI struct {
	service   audit.Interface
	jwtSecret []byte
}

func NewAuditAPI(db *gorm.DB, jwtSecret []byte) *AuditAPI {
	return &AuditAPI{service: audit.NewService(audit.NewRepository(db)), jwtSecret: jwtSecret}
}

func (a *AuditAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// view_audit_log only restricts a caller using Back Office; an Owner /
	// Service Center always sees it.
	rg.GET("/audit-log", middleware.RequireViewPermission(a.jwtSecret, "view_audit_log"), a.ListAuditLog)
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
