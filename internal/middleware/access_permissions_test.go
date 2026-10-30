package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/middleware"
)

// legacyStaffToken mints a staff token the way the API did before per-org
// roles: no access_version claim, only the seven original permission codes.
// It carries org_id so these tests exercise only the permission expansion -
// a real token that old also lacks org_id and is refused outright (see
// TestRequirePermission_TokenWithoutOrgClaim_Returns401).
func legacyStaffToken(t *testing.T, perms ...string) string {
	t.Helper()
	claims := authtoken.StaffClaims{
		StaffID: 7, OrgID: 1, BranchID: 5, RoleID: 3, Permissions: perms,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	require.NoError(t, err)
	return tok
}

func TestRequireStaffPermission(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireStaffPermission(secret, "accept_payments")
	pos := accessToken(t, "pos", &branch)

	t.Run("passes with the permission", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, pos, staffToken(t, "access_pos_portal", "accept_payments")))
	})
	t.Run("is forbidden without it", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(t, gate, pos, staffToken(t, "access_pos_portal")))
	})
	t.Run("is unauthorized without a staff token", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, pos, ""))
	})
	t.Run("a token minted before per-org roles keeps working until it expires", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, pos, legacyStaffToken(t, "access_pos_portal")))
	})
	t.Run("but a legacy token still can't do what its role never could", func(t *testing.T) {
		force := middleware.RequireStaffPermission(secret, "force_close_shift")
		assert.Equal(t, http.StatusForbidden, do(t, force, pos, legacyStaffToken(t, "access_pos_portal")))
		assert.Equal(t, http.StatusOK, do(t, force, pos, legacyStaffToken(t, "access_pos_portal", "access_backoffice")))
	})
}

func TestRequireStaffPermissionOrOrgAdmin(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireStaffPermissionOrOrgAdmin(secret, "add_customers", "manage_customers")
	pos := accessToken(t, "pos", &branch)

	t.Run("owner and service center pass with no staff token", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), ""))
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "service_center", nil), ""))
	})
	t.Run("a till passes with any one of the permissions", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, pos, staffToken(t, "add_customers")))
		assert.Equal(t, http.StatusOK, do(t, gate, pos, staffToken(t, "manage_customers")))
	})
	t.Run("a till without any of them is forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(t, gate, pos, staffToken(t, "access_pos_portal")))
	})
	t.Run("a till with no staff token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, pos, ""))
	})
	t.Run("pre-claim and unknown account types are refused", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "", nil), ""))
		assert.Equal(t, http.StatusForbidden, do(t, gate, accessToken(t, "bogus", nil), staffToken(t, "add_customers")))
	})
	t.Run("an owner's stray staff token is ignored", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), "not-even-a-token"))
	})
}

func TestRequireBackOfficePermission(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireBackOfficePermission(secret, "manage_catalog")
	pos := accessToken(t, "pos", &branch)

	t.Run("owner and service center always pass, with no staff token", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), ""))
		assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "service_center", nil), ""))
	})
	t.Run("a till passes with BOTH the back office switch and the permission", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, pos, staffToken(t, "access_backoffice", "manage_catalog")))
	})
	t.Run("the permission without the switch is forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(t, gate, pos, staffToken(t, "access_pos_portal", "manage_catalog")))
	})
	t.Run("the switch without the permission is forbidden", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, do(t, gate, pos, staffToken(t, "access_backoffice", "manage_stock")))
	})
	t.Run("a till with no staff token is unauthorized", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, pos, ""))
	})
	t.Run("a legacy manager token keeps its reach until it expires", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, do(t, gate, pos, legacyStaffToken(t, "access_pos_portal", "access_backoffice")))
	})
	t.Run("pre-claim and unknown account types are refused", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "", &branch), staffToken(t, "access_backoffice", "manage_catalog")))
		assert.Equal(t, http.StatusForbidden, do(t, gate, accessToken(t, "bogus", nil), staffToken(t, "access_backoffice", "manage_catalog")))
	})
}

// getWith runs a GET through Auth -> gate -> a handler that reports what the
// gate left in the context.
func getWith(t *testing.T, gate gin.HandlerFunc, bearer, staff string) (code int, staffID uint, sawStaff bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(secret))
	r.GET("/x", gate, func(c *gin.Context) {
		staffID, sawStaff = middleware.StaffIDFromContext(c)
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	if staff != "" {
		req.Header.Set("X-Staff-Token", staff)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, staffID, sawStaff
}

func TestRequireViewPermission(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireViewPermission(secret, "view_reports")
	pos := accessToken(t, "pos", &branch)

	t.Run("owner and service center always pass", func(t *testing.T) {
		code, _, _ := getWith(t, gate, accessToken(t, "owner", nil), "")
		assert.Equal(t, http.StatusOK, code)
		code, _, _ = getWith(t, gate, accessToken(t, "service_center", nil), "")
		assert.Equal(t, http.StatusOK, code)
	})
	t.Run("a plain till read with no staff token is unchanged", func(t *testing.T) {
		code, _, saw := getWith(t, gate, pos, "")
		assert.Equal(t, http.StatusOK, code)
		assert.False(t, saw)
	})
	t.Run("a cashier's token is a plain till read, not a back office one", func(t *testing.T) {
		code, id, saw := getWith(t, gate, pos, staffToken(t, "access_pos_portal"))
		assert.Equal(t, http.StatusOK, code)
		assert.True(t, saw, "the staff identity is still recorded for handlers that scope by it")
		assert.Equal(t, uint(7), id)
	})
	t.Run("a back office token needs the view permission", func(t *testing.T) {
		code, _, _ := getWith(t, gate, pos, staffToken(t, "access_backoffice", "view_reports"))
		assert.Equal(t, http.StatusOK, code)
		code, _, _ = getWith(t, gate, pos, staffToken(t, "access_backoffice", "view_sales"))
		assert.Equal(t, http.StatusForbidden, code)
	})
	t.Run("a legacy manager token keeps reading until it expires", func(t *testing.T) {
		code, _, _ := getWith(t, gate, pos, legacyStaffToken(t, "access_pos_portal", "access_backoffice"))
		assert.Equal(t, http.StatusOK, code)
	})
	t.Run("a staff token that is present but invalid is unauthorized", func(t *testing.T) {
		code, _, _ := getWith(t, gate, pos, "garbage")
		assert.Equal(t, http.StatusUnauthorized, code)
	})
	t.Run("a pre-claim token is treated as a plain read, as before", func(t *testing.T) {
		code, _, _ := getWith(t, gate, accessToken(t, "", &branch), "")
		assert.Equal(t, http.StatusOK, code)
	})
}

func TestCallerHasPermission(t *testing.T) {
	branch := uint(5)
	probe := func(bearer, staff string) bool {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(middleware.Auth(secret))
		var got bool
		r.GET("/x", middleware.RequireViewPermission(secret, "view_reports"), func(c *gin.Context) {
			got = middleware.CallerHasPermission(c, "view_item_cost")
			c.Status(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		if staff != "" {
			req.Header.Set("X-Staff-Token", staff)
		}
		r.ServeHTTP(httptest.NewRecorder(), req)
		return got
	}

	assert.True(t, probe(accessToken(t, "owner", nil), ""), "an owner has every permission")
	assert.True(t, probe(accessToken(t, "service_center", nil), ""))
	assert.True(t, probe(accessToken(t, "pos", &branch), staffToken(t, "access_backoffice", "view_reports", "view_item_cost")))
	assert.False(t, probe(accessToken(t, "pos", &branch), staffToken(t, "access_backoffice", "view_reports")))
	assert.False(t, probe(accessToken(t, "pos", &branch), ""), "a till with no staff token has none")
}
