package common

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// HandleError writes err to the response. A RestError (or anything wrapping
// one) is reported with its own status/message/field-errors; anything else
// is treated as an unexpected failure and reported as a bare 500, so a raw
// error from a DB driver or a third-party library never leaks its message
// to the client.
func HandleError(c *gin.Context, err error) {
	var restErr RestError
	if errors.As(err, &restErr) {
		c.JSON(restErr.Status, gin.H{
			"success": false,
			"message": restErr.Message,
			"errors":  restErr.Errors,
		})
		return
	}

	c.JSON(http.StatusInternalServerError, gin.H{
		"success": false,
		"message": "internal server error",
	})
}
