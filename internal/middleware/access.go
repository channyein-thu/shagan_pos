package middleware

import (
	"slices"

	"github.com/gin-gonic/gin"

	"shagan_pos/internal/common"
)

// Account type values as carried in the access token's account_type claim -
// the same strings as identity.AccountType, kept as plain constants here so
// middleware doesn't depend on the identity domain package (the claim is a
// string on the wire anyway).
const (
	AccountTypeOwner         = "owner"
	AccountTypePos           = "pos"
	AccountTypeServiceCenter = "service_center"
)

// BackOfficePermission is the staff permission that unlocks Back Office at a
// terminal (WORKFLOWS section 4).
const BackOfficePermission = "access_backoffice"

// RequireAccountType allows only a bearer token whose account_type claim is
// one of allowed. Stack it after Auth.
//
// A token with no account_type claim (issued before the claim existed) is a
// 401, not a 403: it isn't known to be forbidden, it just needs a fresh
// token from /auth/refresh or /auth/login. A known type outside allowed is
// a 403.
func RequireAccountType(allowed ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountType, ok := AccountTypeFromContext(c)
		if !ok {
			common.HandleError(c, common.UnauthorizedError("token has no account type - refresh or log in again"))
			c.Abort()
			return
		}
		if !slices.Contains(allowed, accountType) {
			common.HandleError(c, common.ForbiddenError("this account type cannot perform this action"))
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequireOrgAdmin allows an Owner or Service Center account only - the
// org-wide logins. Used for staff/branch/device management, which a till
// (POS device token) must never reach, manager PIN or not.
func RequireOrgAdmin() gin.HandlerFunc {
	return RequireAccountType(AccountTypeOwner, AccountTypeServiceCenter)
}

// RequireBackOffice allows an org-wide account (Owner / Service Center)
// outright, or a POS-device token that also carries a valid X-Staff-Token
// whose role grants access_backoffice - a manager who has unlocked Back
// Office at the till. A plain cashier's staff token, or none, is refused.
// Stack it after Auth.
//
// Branch scoping for the POS-device case is NOT done here: the handler
// still has to restrict the request to the token's own branch (see
// cmd/api's scopedBranchID).
func RequireBackOffice(jwtSecret []byte) gin.HandlerFunc {
	requireBackOfficeStaff := RequirePermission(jwtSecret, BackOfficePermission)
	return func(c *gin.Context) {
		accountType, ok := AccountTypeFromContext(c)
		if !ok {
			common.HandleError(c, common.UnauthorizedError("token has no account type - refresh or log in again"))
			c.Abort()
			return
		}
		switch accountType {
		case AccountTypeOwner, AccountTypeServiceCenter:
			c.Next()
		case AccountTypePos:
			requireBackOfficeStaff(c)
		default:
			common.HandleError(c, common.ForbiddenError("this account type cannot perform this action"))
			c.Abort()
		}
	}
}

// RequireStaffTokenOrOrgAdmin is RequireStaffToken for routes where an
// Owner / Service Center account may act directly instead (expenses, void -
// the Owner has no Staff record and no PIN). A POS-device token must still
// carry a valid X-Staff-Token, exactly as with RequireStaffToken; an
// org-wide token passes without one and any X-Staff-Token it sends is
// ignored, so handlers can tell the two apart by whether StaffIDFromContext
// is set. Stack it after Auth.
func RequireStaffTokenOrOrgAdmin(jwtSecret []byte) gin.HandlerFunc {
	requireStaff := RequireStaffToken(jwtSecret)
	return func(c *gin.Context) {
		accountType, ok := AccountTypeFromContext(c)
		if !ok {
			common.HandleError(c, common.UnauthorizedError("token has no account type - refresh or log in again"))
			c.Abort()
			return
		}
		switch accountType {
		case AccountTypeOwner, AccountTypeServiceCenter:
			c.Next()
		case AccountTypePos:
			requireStaff(c)
		default:
			common.HandleError(c, common.ForbiddenError("this account type cannot perform this action"))
			c.Abort()
		}
	}
}
