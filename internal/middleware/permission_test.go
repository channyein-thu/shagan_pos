package middleware_test

import (
	"encoding/json"
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

// PIN verify checks a staff member's org and branch only when it issues the
// token. These tests pin down that every *use* checks them too, so a token
// copied from one till can't act at another branch or another shop.

// preOrgClaimStaffToken mints a staff token the way the API did before
// tokens carried org_id.
func preOrgClaimStaffToken(t *testing.T, perms ...string) string {
	t.Helper()
	claims := authtoken.StaffClaims{
		StaffID: 7, BranchID: 5, RoleID: 3, Permissions: perms, AccessVersion: authtoken.CurrentAccessVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	require.NoError(t, err)
	return tok
}

// doWithError is do, also returning the error message from the body.
func doWithError(t *testing.T, gate gin.HandlerFunc, bearer, staff string) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(secret))
	r.POST("/x", gate, func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("X-Staff-Token", staff)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body.Message
}

func TestRequirePermission_SameOrgAndBranch_Allows(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequirePermission(secret, "approve_void")

	status := do(t, gate, accessToken(t, "pos", &branch), staffTokenAt(t, 1, 5, "approve_void"))

	assert.Equal(t, http.StatusOK, status)
}

func TestRequirePermission_TokenFromAnotherBranch_Returns401(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequirePermission(secret, "approve_void")

	status, msg := doWithError(t, gate, accessToken(t, "pos", &branch), staffTokenAt(t, 1, 9, "approve_void"))

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "staff token is for a different branch", msg)
}

func TestRequirePermission_TokenFromAnotherOrg_Returns401(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequirePermission(secret, "approve_void")

	// Same branch number, different shop: the org is what tells them apart.
	status, msg := doWithError(t, gate, accessToken(t, "pos", &branch), staffTokenAt(t, 2, 5, "approve_void"))

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "staff token is for a different organization", msg)
}

func TestRequirePermission_TokenWithoutOrgClaim_Returns401(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequirePermission(secret, "approve_void")

	status, msg := doWithError(t, gate, accessToken(t, "pos", &branch), preOrgClaimStaffToken(t, "approve_void"))

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "staff token is out of date, sign in again with your PIN", msg)
}

func TestRequirePermission_OrgWideAccessToken_ChecksOrgOnly(t *testing.T) {
	gate := middleware.RequirePermission(secret, "approve_void")

	// An owner's access token has no branch, so any branch of their own org passes...
	assert.Equal(t, http.StatusOK, do(t, gate, accessToken(t, "owner", nil), staffTokenAt(t, 1, 9, "approve_void")))
	// ...but another org's never does.
	assert.Equal(t, http.StatusUnauthorized, do(t, gate, accessToken(t, "owner", nil), staffTokenAt(t, 2, 9, "approve_void")))
}

func TestRequireStaffToken_TokenFromAnotherBranch_Returns401(t *testing.T) {
	branch := uint(5)
	gate := middleware.RequireStaffToken(secret)

	status := do(t, gate, accessToken(t, "pos", &branch), staffTokenAt(t, 1, 9, "access_pos_portal"))

	assert.Equal(t, http.StatusUnauthorized, status)
}

// approved runs one request at the till (org 1, branch 5) carrying
// approvalToken and reports what ManagerApproved said.
func approved(t *testing.T, approvalToken string) bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Auth(secret))
	var got bool
	r.POST("/x", func(c *gin.Context) {
		got = middleware.ManagerApproved(c, secret, "approve_void")
		c.Status(http.StatusOK)
	})

	branch := uint(5)
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken(t, "pos", &branch))
	req.Header.Set("X-Manager-Approval-Token", approvalToken)
	r.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

func TestManagerApproved_SameBranch_ReturnsTrue(t *testing.T) {
	assert.True(t, approved(t, staffTokenAt(t, 1, 5, "approve_void")))
}

func TestManagerApproved_ApprovalFromAnotherBranch_ReturnsFalse(t *testing.T) {
	assert.False(t, approved(t, staffTokenAt(t, 1, 9, "approve_void")))
}

func TestManagerApproved_ApprovalFromAnotherOrg_ReturnsFalse(t *testing.T) {
	assert.False(t, approved(t, staffTokenAt(t, 2, 5, "approve_void")))
}

func TestManagerApproved_ApprovalWithoutOrgClaim_ReturnsFalse(t *testing.T) {
	assert.False(t, approved(t, preOrgClaimStaffToken(t, "approve_void")))
}
