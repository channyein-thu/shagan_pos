package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/audit"
	"shagan_pos/internal/common"
	"shagan_pos/internal/subscription"
)

// subscriptionServices holds one subscription.Service per database. Everything
// that reads an organization's plan - /me, /sync/catalog, RequireFeature, the
// limit checks and the internal portal - must share ONE instance, because the
// per-org entitlements cache lives inside it and an /internal write clears it
// there. Each API constructor asks for it here instead of building its own.
var subscriptionServices sync.Map // *gorm.DB -> *subscription.Service

func sharedSubscription(db *gorm.DB) *subscription.Service {
	if s, ok := subscriptionServices.Load(db); ok {
		return s.(*subscription.Service)
	}
	s := subscription.NewService(subscription.NewRepository(db), audit.NewRepository(db), db)
	actual, _ := subscriptionServices.LoadOrStore(db, s)
	return actual.(*subscription.Service)
}

// SubscriptionAPI is the Shagan-team side of subscription tiers - mounted on
// /internal only (X-Internal-Key); a tenant can't read or change its own plan.
type SubscriptionAPI struct {
	service subscription.Interface
}

func NewSubscriptionAPI(db *gorm.DB) *SubscriptionAPI {
	return &SubscriptionAPI{service: sharedSubscription(db)}
}

func (a *SubscriptionAPI) RegisterInternalRoutes(rg *gin.RouterGroup) {
	rg.GET("/plans", a.ListPlans)
	rg.GET("/organizations/:id/subscription", a.GetSubscription)
	rg.PATCH("/organizations/:id/subscription", a.UpdateSubscription)
	rg.PUT("/organizations/:id/features/:code", a.SetFeatureOverride)
	rg.DELETE("/organizations/:id/features/:code", a.ClearFeatureOverride)
}

// ListPlans handles `GET /internal/plans`: the tier catalogue and every
// feature code an override may name.
func (a *SubscriptionAPI) ListPlans(c *gin.Context) {
	c.JSON(http.StatusOK, a.service.ListPlans())
}

func orgIDParam(c *gin.Context) (uint, bool) {
	v, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || v == 0 {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return 0, false
	}
	return uint(v), true
}

// GetSubscription handles `GET /internal/organizations/:id/subscription`.
func (a *SubscriptionAPI) GetSubscription(c *gin.Context) {
	orgID, ok := orgIDParam(c)
	if !ok {
		return
	}
	result, err := a.service.GetSubscription(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateSubscription handles `PATCH /internal/organizations/:id/subscription`.
// Partial: an omitted field is unchanged, "expires_at": null clears the end
// date. changed_by (the admin's email) is required.
func (a *SubscriptionAPI) UpdateSubscription(c *gin.Context) {
	orgID, ok := orgIDParam(c)
	if !ok {
		return
	}
	var in subscription.UpdateSubscriptionRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateSubscription(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SetFeatureOverride handles `PUT /internal/organizations/:id/features/:code`:
// {"enabled": true|false, "changed_by": "...", "note": "..."}.
func (a *SubscriptionAPI) SetFeatureOverride(c *gin.Context) {
	orgID, ok := orgIDParam(c)
	if !ok {
		return
	}
	var in subscription.FeatureOverrideRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.SetFeatureOverride(c.Request.Context(), orgID, c.Param("code"), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ClearFeatureOverride handles `DELETE /internal/organizations/:id/features/:code`:
// the feature goes back to the plan's default. changed_by comes from an
// optional JSON body or, as a DELETE often has none, the ?changed_by= query.
func (a *SubscriptionAPI) ClearFeatureOverride(c *gin.Context) {
	orgID, ok := orgIDParam(c)
	if !ok {
		return
	}
	var in subscription.FeatureOverrideRequest
	if err := c.ShouldBindJSON(&in); err != nil && !errors.Is(err, io.EOF) {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	if in.ChangedBy == "" {
		in.ChangedBy = c.Query("changed_by")
	}
	if in.Note == "" {
		in.Note = c.Query("note")
	}
	result, err := a.service.ClearFeatureOverride(c.Request.Context(), orgID, c.Param("code"), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
