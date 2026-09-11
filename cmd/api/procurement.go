package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/procurement"
)

type ProcurementAPI struct {
	service procurement.Interface
}

func NewProcurementAPI(db *gorm.DB) *ProcurementAPI {
	return &ProcurementAPI{service: procurement.NewService(procurement.NewRepository(db))}
}

func (a *ProcurementAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/procurement")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real procurement listing endpoint.
func (a *ProcurementAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
