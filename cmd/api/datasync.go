package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/datasync"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/inventory"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/sales"
	"shagan_pos/internal/storage"
)

type SyncAPI struct {
	service datasync.Interface
}

func NewSyncAPI(db *gorm.DB, store storage.Storage) *SyncAPI {
	return &SyncAPI{
		service: datasync.NewService(
			datasync.NewRepository(db),
			identity.NewRepository(db),
			catalog.NewService(catalog.NewRepository(db), identity.NewRepository(db), db, store),
			sales.NewService(sales.NewRepository(db), inventory.NewRepository(db), audit.NewRepository(db), db),
			db,
		),
	}
}

func (a *SyncAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sync/catalog", a.GetCatalogSnapshot)
	rg.POST("/sync/sales", a.IngestQueuedSales)
	rg.GET("/sync/status", a.GetSyncStatus)
	rg.GET("/sync/conflicts", a.ListSyncConflicts)
	rg.PATCH("/sync/conflicts/:id/resolve", a.ResolveSyncConflict)
}

// GetCatalogSnapshot handles `GET /sync/catalog`. Snapshot + ETag for
// offline caching (products/categories/combos) - restricted to the
// caller's own branch when the token carries one, org-wide otherwise, same
// reasoning as inventory.ListStockLevels. Optional ?branch_id= (owner/
// service_center only). A matching If-None-Match gets a 304 with no body
// instead of re-sending an unchanged snapshot.
func (a *SyncAPI) GetCatalogSnapshot(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	snapshot, etag, err := a.service.GetCatalogSnapshot(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("ETag", etag)
	c.JSON(http.StatusOK, snapshot)
}

// IngestQueuedSales handles `POST /sync/sales`. Queued-sale ingest,
// idempotent by each sale's own client-generated ID - see
// datasync.Interface.IngestQueuedSales's doc. branchID comes from the
// caller's own pos-device access token, never client input, same
// reasoning as sales.CreateSale.
func (a *SyncAPI) IngestQueuedSales(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := requireBranchID(c)
	if !ok {
		return
	}
	var in datasync.IngestQueuedSalesRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.IngestQueuedSales(c.Request.Context(), orgID, branchID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetSyncStatus handles `GET /sync/status`. Same scoping as
// GetCatalogSnapshot.
func (a *SyncAPI) GetSyncStatus(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.GetSyncStatus(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListSyncConflicts handles `GET /sync/conflicts`. Same scoping as
// GetCatalogSnapshot.
func (a *SyncAPI) ListSyncConflicts(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListSyncConflicts(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ResolveSyncConflict handles `PATCH /sync/conflicts/:id/resolve`. Sets
// resolved_by/resolved_at - see datasync.Interface.ResolveSyncConflict's
// doc for what "resolved" means here.
func (a *SyncAPI) ResolveSyncConflict(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	actorUserID, ok := middleware.UserIDFromContext(c)
	if !ok {
		common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ResolveSyncConflict(c.Request.Context(), orgID, actorUserID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
