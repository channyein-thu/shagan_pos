package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/platform"
)

type PlatformAPI struct {
	service platform.Interface
}

func NewPlatformAPI(db *gorm.DB) *PlatformAPI {
	return &PlatformAPI{service: platform.NewService(platform.NewRepository(db))}
}

func (a *PlatformAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/receipt-settings", a.GetReceiptSettings)
	rg.PUT("/receipt-settings", a.UpdateReceiptSettings)
	rg.POST("/printers/test", a.TestPrinter)
}

// GetReceiptSettings handles `GET /receipt-settings`. Needed for the edit form's pre-fill / live preview
func (a *PlatformAPI) GetReceiptSettings(c *gin.Context) {
	result, err := a.service.GetReceiptSettings(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateReceiptSettings handles `PUT /receipt-settings`.
func (a *PlatformAPI) UpdateReceiptSettings(c *gin.Context) {
	var in platform.UpdateReceiptSettingsRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateReceiptSettings(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// TestPrinter handles `POST /printers/test`. No table; renders a test payload
func (a *PlatformAPI) TestPrinter(c *gin.Context) {
	result, err := a.service.TestPrinter(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
