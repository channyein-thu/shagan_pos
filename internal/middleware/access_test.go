package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/middleware"
)

var secret = []byte("test-secret")

func accessToken(t *testing.T, accountType string, branchID *uint) string {
	t.Helper()
	tok, err := authtoken.GenerateAccessToken(secret, 1, 1, accountType, branchID, time.Minute)
	require.NoError(t, err)
	return tok
}

func staffToken(t *testing.T, perms ...string) string {
	t.Helper()
	tok, err := authtoken.GenerateStaffToken(secret, 7, 5, 3, perms, time.Minute)
	require.NoError(t, err)
	return tok
}

// do runs one request through Auth -> gate -> a handler that returns 200,
// returning the status code.
func do(t *testing.T, gate gin.HandlerFunc, bearer, staff string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(secret))
	r.POST("/x", gate, func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	if staff != "" {
		req.Header.Set("X-Staff-Token", staff)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRequireOrgAdmin(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireOrgAdmin()

	assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), ""))
	assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "service_center", nil), ""))
	// A till's token is refused even with a manager's staff token.
	assert.Equal(t, http.StatusForbidden, do(t, gate, accessToken(t, "pos", &branch), staffToken(t, "access_backoffice")))
	// Pre-claim token: re-authenticate, not forbidden.
	assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "", nil), ""))
}

func TestRequireBackOffice(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireBackOffice(secret)

	t.Run("owner and service center pass without a staff token", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), ""))
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "service_center", nil), ""))
	})
	t.Run("pos token with manager staff token passes", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "pos", &branch), staffToken(t, "access_pos_portal", "access_backoffice")))
	})
	t.Run("pos token with cashier staff token is forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(t, gate, accessToken(t, "pos", &branch), staffToken(t, "access_pos_portal")))
	})
	t.Run("pos token with no staff token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "pos", &branch), ""))
	})
	t.Run("pre-claim token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "", &branch), staffToken(t, "access_backoffice")))
	})
	t.Run("unknown account type is forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(t, gate, accessToken(t, "bogus", nil), staffToken(t, "access_backoffice")))
	})
}

func TestRequireStaffTokenOrOrgAdmin(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireStaffTokenOrOrgAdmin(secret)

	t.Run("owner and service center pass with no staff token", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), ""))
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "service_center", nil), ""))
	})
	t.Run("pos token with any valid staff token passes", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "pos", &branch), staffToken(t, "access_pos_portal")))
	})
	t.Run("pos token with no staff token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "pos", &branch), ""))
	})
	t.Run("pre-claim token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "", nil), ""))
	})
	t.Run("an owner's stray staff token is ignored, not honoured as staff identity", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(middleware.Auth(secret))
		var sawStaff bool
		r.POST("/x", gate, func(c *gin.Context) {
			_, sawStaff = middleware.StaffIDFromContext(c)
			c.Status(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken(t, "owner", nil))
		req.Header.Set("X-Staff-Token", staffToken(t, "access_backoffice"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.False(t, sawStaff, "handlers rely on this to tell an owner from staff")
	})
}
