package middleware

import (
	"slices"

	"github.com/gin-gonic/gin"

	"shagan_pos/internal/authtoken"
	"shagan_pos/internal/common"
)

// Context keys RequirePermission/RequireStaffToken set on a successfully
// authorized request.
const (
	ContextStaffID          = "staff_id"
	ContextRoleID           = "role_id"
	ContextStaffPermissions = "staff_permissions"
)

// staffTokenHeader carries a separate token from Auth's own Authorization
// header - a request can need both: which device/org is calling (Auth), and
// which human is authorized to do this (RequirePermission). Raw token value,
// no "Bearer " prefix - same convention as InternalAuth's X-Internal-Key.
const staffTokenHeader = "X-Staff-Token"

// RequirePermission validates the X-Staff-Token JWT issued by
// identity.Service.VerifyStaffPIN/VerifyManagerPIN and requires that its
// embedded Permissions include permission. Stack this after Auth on routes
// that need staff-level authorization, not globally - most routes only need
// Auth.
//
// A missing/invalid/expired token is a 401 (authentication failure - we don't
// know who's asking). A valid token whose role just doesn't grant permission
// is a 403, deliberately different from VerifyStaffPIN/VerifyManagerPIN's
// generic 401: those endpoints hide failures because the caller is guessing
// someone else's PIN, but here the caller already holds their own verified
// token, so telling them their own role lacks a permission leaks nothing
// about anyone else.
func RequirePermission(jwtSecret []byte, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseStaffToken(c, jwtSecret)
		if !ok {
			return
		}

		if !slices.Contains(claims.Permissions, permission) {
			common.HandleError(c, common.ForbiddenError("insufficient permission"))
			c.Abort()
			return
		}

		setStaffContext(c, claims)
		c.Next()
	}
}

// RequireStaffToken validates the same X-Staff-Token JWT as RequirePermission
// and identifies which staff member is acting, without requiring any one
// specific granted permission - stack this on routes that just need to know
// *who* is calling (e.g. "record this expense as logged by me", "only the
// staff who opened this shift may close it"), as opposed to
// RequirePermission's *are they allowed to do this specific thing*.
func RequireStaffToken(jwtSecret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := parseStaffToken(c, jwtSecret)
		if !ok {
			return
		}
		setStaffContext(c, claims)
		c.Next()
	}
}

func parseStaffToken(c *gin.Context, jwtSecret []byte) (*authtoken.StaffClaims, bool) {
	token := c.GetHeader(staffTokenHeader)
	if token == "" {
		common.HandleError(c, common.UnauthorizedError("missing staff token"))
		c.Abort()
		return nil, false
	}

	claims, err := authtoken.ParseStaffToken(jwtSecret, token)
	if err != nil {
		common.HandleError(c, common.UnauthorizedError("invalid or expired staff token"))
		c.Abort()
		return nil, false
	}
	return claims, true
}

func setStaffContext(c *gin.Context, claims *authtoken.StaffClaims) {
	c.Set(ContextStaffID, claims.StaffID)
	c.Set(ContextRoleID, claims.RoleID)
	c.Set(ContextStaffPermissions, claims.Permissions)
}

// StaffIDFromContext returns the authorized caller's staff ID, as set by
// RequirePermission.
func StaffIDFromContext(c *gin.Context) (uint, bool) {
	v, ok := c.Get(ContextStaffID)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

// RoleIDFromContext returns the authorized caller's role ID, as set by
// RequirePermission.
func RoleIDFromContext(c *gin.Context) (uint, bool) {
	v, ok := c.Get(ContextRoleID)
	if !ok {
		return 0, false
	}
	id, ok := v.(uint)
	return id, ok
}

// StaffHasPermission returns whether the authorized caller's role (as set by
// RequirePermission/RequireStaffToken) includes permission - e.g. checking
// for "access_backoffice" to decide whether a Manager may act on another
// staff member's record, versus an ordinary Staff member who may only act on
// their own.
func StaffHasPermission(c *gin.Context, permission string) bool {
	v, ok := c.Get(ContextStaffPermissions)
	if !ok {
		return false
	}
	perms, ok := v.([]string)
	return ok && slices.Contains(perms, permission)
}
