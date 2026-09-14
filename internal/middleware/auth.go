package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
)

// Context keys Auth sets on a successfully authenticated request.
const (
	ContextUserID   = "user_id"
	ContextOrgID    = "org_id"
	ContextBranchID = "branch_id"
)

const bearerPrefix = "Bearer "

// Auth validates the bearer JWT access token issued by identity.Service.Login
// and attaches the caller's user_id/org_id to the request context for
// handlers to read.
//
// This only proves *who* is calling (authentication) - it doesn't yet check
// *what* they're allowed to do. Authorization (role/permission checks) needs
// staff PIN login to exist first, since Roles/Permissions are tied to Staff,
// not the owner User this token represents - see identity.VerifyStaffPIN.
func Auth(jwtSecret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, bearerPrefix) {
			common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(header, bearerPrefix)
		claims, err := authtoken.ParseAccessToken(jwtSecret, tokenString)
		if err != nil {
			common.HandleError(c, common.UnauthorizedError("invalid or expired token"))
			c.Abort()
			return
		}

		c.Set(ContextUserID, claims.UserID)
		c.Set(ContextOrgID, claims.OrgID)
		if claims.BranchID != nil {
			c.Set(ContextBranchID, *claims.BranchID)
		}
		c.Next()
	}
}

// UserIDFromContext returns the authenticated caller's user ID, as set by Auth.
func UserIDFromContext(c *gin.Context) (uint, bool) {
	v, ok := c.Get(ContextUserID)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

// OrgIDFromContext returns the authenticated caller's organization ID, as set by Auth.
func OrgIDFromContext(c *gin.Context) (uint, bool) {
	v, ok := c.Get(ContextOrgID)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

// BranchIDFromContext returns the authenticated caller's branch ID, as set by
// Auth. Only present for a pos-account token - false for owner/service_center
// callers, which are org-wide (see identity.User.BranchID).
func BranchIDFromContext(c *gin.Context) (uint, bool) {
	v, ok := c.Get(ContextBranchID)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}
