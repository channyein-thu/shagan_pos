package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/common"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/subscription"
)

// fakeChecker answers every org with the entitlements it holds, and records
// which org was asked about.
type fakeChecker struct {
	ent   subscription.Entitlements
	err   error
	asked []uint
}

func (f *fakeChecker) Entitlements(_ context.Context, orgID uint, _ time.Time) (*subscription.Entitlements, error) {
	f.asked = append(f.asked, orgID)
	if f.err != nil {
		return nil, f.err
	}
	ent := f.ent
	return &ent, nil
}

// callGated runs one request through Auth -> RequireFeature -> a handler that
// answers 200 (a dummy route: nothing real is gated yet).
func callGated(t *testing.T, checker middleware.FeatureChecker, feature string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(secret))
	r.GET("/gated", middleware.RequireFeature(checker, feature), func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken(t, "owner", nil))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequireFeature_FeatureNotInThePlan_Is403FeatureNotInPlan(t *testing.T) {
	checker := &fakeChecker{ent: subscription.Entitlements{Plan: "basic", Features: []string{}}}

	w := callGated(t, checker, subscription.FeaturePromotions)

	require.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, false, body["success"])
	require.Equal(t, "feature_not_in_plan", body["code"])
	require.Equal(t, "promotions", body["feature"])
	require.Equal(t, "basic", body["plan"])
	require.NotEmpty(t, body["message"])
	require.Equal(t, []uint{1}, checker.asked, "asked about the token's own org")
}

func TestRequireFeature_FeatureInThePlan_PassesThrough(t *testing.T) {
	checker := &fakeChecker{ent: subscription.Entitlements{Plan: "pro", Features: []string{"promotions", "report_export"}}}

	w := callGated(t, checker, subscription.FeaturePromotions)

	require.Equal(t, http.StatusOK, w.Code)
}

// The plan reported is the EFFECTIVE one - a Pro that lapsed reads "basic".
func TestRequireFeature_ReportsTheEffectivePlan(t *testing.T) {
	ent := subscription.Resolve(subscription.OrgPlan{Plan: "pro", ExpiresAt: ptrTime(time.Now().Add(-30 * 24 * time.Hour))}, nil, time.Now())
	w := callGated(t, &fakeChecker{ent: ent}, subscription.FeaturePromotions)

	require.Equal(t, http.StatusForbidden, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "basic", body["plan"])
}

// A per-org override on turns a feature on whatever the plan says.
func TestRequireFeature_HonoursAFeatureOverride(t *testing.T) {
	ent := subscription.Resolve(subscription.OrgPlan{Plan: "basic"},
		[]subscription.OrgFeatureOverride{{Feature: subscription.FeatureLoyalty, Enabled: true}}, time.Now())

	require.Equal(t, http.StatusOK, callGated(t, &fakeChecker{ent: ent}, subscription.FeatureLoyalty).Code)
	require.Equal(t, http.StatusForbidden, callGated(t, &fakeChecker{ent: ent}, subscription.FeaturePromotions).Code)
}

func TestRequireFeature_ALookupFailure_IsNotMistakenForAMissingFeature(t *testing.T) {
	w := callGated(t, &fakeChecker{err: errors.New("db down")}, subscription.FeaturePromotions)
	require.Equal(t, http.StatusInternalServerError, w.Code)

	w = callGated(t, &fakeChecker{err: common.NotFoundError("organization not found")}, subscription.FeaturePromotions)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func ptrTime(t time.Time) *time.Time { return &t }
