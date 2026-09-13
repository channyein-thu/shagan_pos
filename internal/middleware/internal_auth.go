package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"shagan_pos/internal/common"
)

// InternalAuth restricts a route group to callers that know a shared secret
// (INTERNAL_API_KEY), for endpoints that must exist before any org/user/staff
// account does - so the normal staff-PIN/JWT auth can't apply yet. Checked via
// constant-time comparison to avoid leaking the key through response timing.
func InternalAuth(expectedKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if expectedKey == "" {
			common.JSONError(c, http.StatusUnauthorized, "internal_auth_not_configured", "INTERNAL_API_KEY is not set")
			c.Abort()
			return
		}

		got := c.GetHeader("X-Internal-Key")
		if subtle.ConstantTimeCompare([]byte(got), []byte(expectedKey)) != 1 {
			common.JSONError(c, http.StatusUnauthorized, "unauthorized", "invalid internal key")
			c.Abort()
			return
		}

		c.Next()
	}
}
