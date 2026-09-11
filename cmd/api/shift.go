package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/shift"
)

type ShiftAPI struct {
	service shift.Interface
}

func NewShiftAPI(db *gorm.DB) *ShiftAPI {
	return &ShiftAPI{service: shift.NewService(shift.NewRepository(db))}
}

func (a *ShiftAPI) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/shifts")
	g.GET("", a.ListHandler)
}

// ListHandler is a placeholder. TODO: replace with the real shift listing endpoint.
func (a *ShiftAPI) ListHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
