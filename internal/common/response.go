package common

import "github.com/gin-gonic/gin"

// APIError is the standard JSON error envelope returned by the API.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// JSONError writes a standard error envelope to the response.
func JSONError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": APIError{Code: code, Message: message}})
}
