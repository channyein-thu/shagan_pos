package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Auth is a placeholder JWT auth middleware.
// TODO: validate the bearer token (see Sessions/Devices in the ERD) and load the staff/user context.
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}
		c.Next()
	}
}
