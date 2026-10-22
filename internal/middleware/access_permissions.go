package middleware

import (
	"slices"

	"github.com/gin-gonic/gin"

	"shagan_pos/internal/common"
)

// Role & Access Management guards. Each permission an Owner can tick in the
// roles screen is enforced by one of these, stacked after Auth:
//
//   - RequireStaffPermission - a till action: the X-Staff-Token must carry it.
//   - RequireStaffPermissionOrOrgAdmin - the same, but an Owner / Service
//     Center acting directly passes (they have no Staff record or PIN).
//   - RequireBackOfficePermission - a back office write: Owner always passes; a
//     till needs a staff token carrying BOTH the Back office switch
//     (access_backoffice) and the permission.
//   - RequireViewPermission - a back office read: only restricts a caller who
//     is *using Back Office* (a staff token with access_backoffice); plain
//     till reads are unchanged.

// RequireStaffPermission validates the X-Staff-Token and requires permission.
// It is RequirePermission under the name the roles screen uses.
func RequireStaffPermission(jwtSecret []byte, permission string) gin.HandlerFunc {
	return RequirePermission(jwtSecret, permission)
}

// RequireStaffPermissionOrOrgAdmin lets an Owner / Service Center through
// outright (any X-Staff-Token they send is ignored, so handlers can tell the
// two apart by whether StaffIDFromContext is set) and requires a POS-device
// token's staff token to carry at least one of permissions.
func RequireStaffPermissionOrOrgAdmin(jwtSecret []byte, permissions ...string) gin.HandlerFunc {
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
			claims, ok := parseStaffToken(c, jwtSecret)
			if !ok {
				return
			}
			granted := claims.EffectivePermissions()
			if !slices.ContainsFunc(permissions, func(p string) bool { return slices.Contains(granted, p) }) {
				common.HandleError(c, common.ForbiddenError("insufficient permission"))
				c.Abort()
				return
			}
			setStaffContext(c, claims)
			c.Next()
		default:
			common.HandleError(c, common.ForbiddenError("this account type cannot perform this action"))
			c.Abort()
		}
	}
}

// RequireBackOfficePermission is RequireBackOffice narrowed to one permission:
// an Owner / Service Center passes outright, and a POS-device token passes
// only with an X-Staff-Token whose role grants both access_backoffice (the
// Back office switch) and permission. Branch scoping for the POS case is still
// the handler's job (see cmd/api's scopedBranchID).
func RequireBackOfficePermission(jwtSecret []byte, permission string) gin.HandlerFunc {
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
			claims, ok := parseStaffToken(c, jwtSecret)
			if !ok {
				return
			}
			granted := claims.EffectivePermissions()
			if !slices.Contains(granted, BackOfficePermission) || !slices.Contains(granted, permission) {
				common.HandleError(c, common.ForbiddenError("insufficient permission"))
				c.Abort()
				return
			}
			setStaffContext(c, claims)
			c.Next()
		default:
			common.HandleError(c, common.ForbiddenError("this account type cannot perform this action"))
			c.Abort()
		}
	}
}

// RequireViewPermission guards a read that Back Office shows but the till
// also uses (stock levels, the customer list, the sales list ...). It only
// restricts a caller who is using Back Office - a POS-device token carrying an
// X-Staff-Token with access_backoffice - who then needs permission. An Owner /
// Service Center always passes, and a till with no staff token, or a cashier's
// token (no access_backoffice), is a plain till read and is left alone.
//
// A valid staff token is recorded in the request context either way, so a
// handler can still scope by who is asking (e.g. a cashier without
// view_all_receipts sees only their own sales). A staff token that is present
// but invalid is a 401. A token with no account type (issued before that claim
// existed) is treated as a plain read, as these routes always have.
func RequireViewPermission(jwtSecret []byte, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountType, _ := AccountTypeFromContext(c)
		if accountType != AccountTypePos {
			c.Next()
			return
		}
		if c.GetHeader(staffTokenHeader) == "" {
			c.Next()
			return
		}
		claims, ok := parseStaffToken(c, jwtSecret)
		if !ok {
			return
		}
		granted := claims.EffectivePermissions()
		if slices.Contains(granted, BackOfficePermission) && !slices.Contains(granted, permission) {
			common.HandleError(c, common.ForbiddenError("insufficient permission"))
			c.Abort()
			return
		}
		setStaffContext(c, claims)
		c.Next()
	}
}

// CallerHasPermission reports whether the caller may do what permission
// guards: an Owner / Service Center always may (they hold every permission
// implicitly), a till only if its staff token (as recorded by
// RequirePermission / RequireStaffToken / RequireViewPermission) carries it.
func CallerHasPermission(c *gin.Context, permission string) bool {
	if accountType, ok := AccountTypeFromContext(c); ok &&
		(accountType == AccountTypeOwner || accountType == AccountTypeServiceCenter) {
		return true
	}
	return StaffHasPermission(c, permission)
}
