package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/identity"
)

type IdentityAPI struct {
	service identity.Interface
}

func NewIdentityAPI(db *gorm.DB) *IdentityAPI {
	return &IdentityAPI{service: identity.NewService(identity.NewRepository(db))}
}

func (a *IdentityAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/identity")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real identity listing endpoint.
func (a *IdentityAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
