package middleware

import (
	"github.com/gin-gonic/gin"

	"shagan_pos/internal/common"
)

// Auth is a placeholder JWT auth middleware.
// TODO: validate the bearer token (see Sessions/Devices in the ERD) and load the staff/user context.
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			common.HandleError(c, common.UnauthorizedError("missing authorization header"))
			c.Abort()
			return
		}
		c.Next()
	}
}
