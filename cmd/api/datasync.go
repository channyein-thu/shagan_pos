package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/datasync"
)

type SyncAPI struct {
	service datasync.Interface
}

func NewSyncAPI(db *gorm.DB) *SyncAPI {
	return &SyncAPI{service: datasync.NewService(datasync.NewRepository(db))}
}

func (a *SyncAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sync/catalog", a.GetCatalogSnapshot)
	rg.POST("/sync/sales", a.IngestQueuedSales)
	rg.POST("/sync/flush", a.FlushSync)
	rg.GET("/sync/status", a.GetSyncStatus)
	rg.GET("/sync/conflicts", a.ListSyncConflicts)
	rg.PATCH("/sync/conflicts/:id/resolve", a.ResolveSyncConflict)
}

// GetCatalogSnapshot handles `GET /sync/catalog`. Snapshot + ETag for offline caching (products/categories/combos)
func (a *SyncAPI) GetCatalogSnapshot(c *gin.Context) {
	result, err := a.service.GetCatalogSnapshot(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// IngestQueuedSales handles `POST /sync/sales`. Queued-sale ingest, idempotent
func (a *SyncAPI) IngestQueuedSales(c *gin.Context) {
	result, err := a.service.IngestQueuedSales(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// FlushSync handles `POST /sync/flush`.
func (a *SyncAPI) FlushSync(c *gin.Context) {
	result, err := a.service.FlushSync(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSyncStatus handles `GET /sync/status`.
func (a *SyncAPI) GetSyncStatus(c *gin.Context) {
	result, err := a.service.GetSyncStatus(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListSyncConflicts handles `GET /sync/conflicts`.
func (a *SyncAPI) ListSyncConflicts(c *gin.Context) {
	result, err := a.service.ListSyncConflicts(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResolveSyncConflict handles `PATCH /sync/conflicts/:id/resolve`. Sets resolved_by/resolved_at
func (a *SyncAPI) ResolveSyncConflict(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ResolveSyncConflict(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
