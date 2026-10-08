package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// corsAllowedHeaders is every request header a browser client of this API may
// send: the bearer token, the JSON body type, the staff-PIN and manager-
// approval tokens (docs/WORKFLOWS.md section 2), and If-None-Match for the
// offline catalog's ETag. Fixed, never echoed back from the request.
const corsAllowedHeaders = "Authorization, Content-Type, X-Staff-Token, X-Manager-Approval-Token, If-None-Match"

// ParseCORSOrigins turns the comma-separated CORS_ALLOWED_ORIGINS value into
// a clean list: whitespace and trailing slashes trimmed, empties dropped.
func ParseCORSOrigins(raw string) []string {
	var origins []string
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" {
			origins = append(origins, o)
		}
	}
	return origins
}

// CORS lets a browser on one of allowedOrigins call this API cross-origin.
//
// Only an exact Origin match gets the Access-Control-* headers; any other
// origin gets none, so the browser blocks the call. An empty list therefore
// means "no browser may call cross-origin", which is the right default: the
// Next.js frontends proxy server-side (no CORS involved), and Postman/curl
// send no Origin at all and are never affected. A literal "*" entry opts back
// into "any origin" - for local development only. No credentials/cookies are
// ever allowed: the API authenticates with bearer tokens, so there is
// nothing for a foreign page to ride on.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[strings.ToLower(o)] = struct{}{}
	}

	return func(c *gin.Context) {
		// The response varies by Origin, so a shared cache must not serve one
		// origin's CORS headers to another.
		c.Writer.Header().Add("Vary", "Origin")

		origin := c.GetHeader("Origin")
		if origin != "" {
			_, ok := allowed[strings.ToLower(origin)]
			if allowAll || ok {
				if allowAll {
					c.Header("Access-Control-Allow-Origin", "*")
				} else {
					c.Header("Access-Control-Allow-Origin", origin)
				}
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				c.Header("Access-Control-Allow-Headers", corsAllowedHeaders)
				// ETag is how a till knows its cached catalog is current; a
				// cross-origin page can only read it if it's exposed.
				c.Header("Access-Control-Expose-Headers", "ETag")
				c.Header("Access-Control-Max-Age", "600")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
