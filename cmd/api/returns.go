package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/returns"
)

type ReturnsAPI struct {
	service returns.Interface
}

func NewReturnsAPI(db *gorm.DB) *ReturnsAPI {
	return &ReturnsAPI{service: returns.NewService(returns.NewRepository(db))}
}

func (a *ReturnsAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/returns")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real returns listing endpoint.
func (a *ReturnsAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
