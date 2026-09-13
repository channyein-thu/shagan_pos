package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/platform"
	"shagan_pos/internal/storage"
)

type PlatformAPI struct {
	service platform.Interface
}

func NewPlatformAPI(db *gorm.DB, store storage.Storage) *PlatformAPI {
	return &PlatformAPI{service: platform.NewService(platform.NewRepository(db, store))}
}

func (a *PlatformAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/receipt-settings", a.GetReceiptSettings)
	rg.PUT("/receipt-settings", a.UpdateReceiptSettings)
	rg.POST("/printers/test", a.TestPrinter)
	rg.POST("/branches/:id/payment-qr-codes", a.UploadPaymentQRCode)
	rg.GET("/branches/:id/payment-qr-codes", a.ListPaymentQRCodes)
	rg.DELETE("/branches/:id/payment-qr-codes/:qrId", a.DeletePaymentQRCode)
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

// UploadPaymentQRCode handles `POST /branches/:id/payment-qr-codes`. Multipart
// form: "file" (the QR image) + "provider" (e.g. "kbzpay", "wavepay").
func (a *PlatformAPI) UploadPaymentQRCode(c *gin.Context) {
	branchID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}

	provider := c.PostForm("provider")
	if provider == "" {
		common.HandleError(c, common.BadRequestError("provider is required"))
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.HandleError(c, common.BadRequestError("file is required"))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		common.HandleError(c, common.BadRequestError("could not read file"))
		return
	}
	defer file.Close()

	result, err := a.service.UploadPaymentQRCode(
		c.Request.Context(),
		uint(branchID),
		provider,
		file,
		fileHeader.Size,
		fileHeader.Header.Get("Content-Type"),
	)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListPaymentQRCodes handles `GET /branches/:id/payment-qr-codes`.
func (a *PlatformAPI) ListPaymentQRCodes(c *gin.Context) {
	branchID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.ListPaymentQRCodes(c.Request.Context(), uint(branchID))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeletePaymentQRCode handles `DELETE /branches/:id/payment-qr-codes/:qrId`.
func (a *PlatformAPI) DeletePaymentQRCode(c *gin.Context) {
	branchID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	qrID, err := strconv.ParseUint(c.Param("qrId"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid qrId"))
		return
	}
	if err := a.service.DeletePaymentQRCode(c.Request.Context(), uint(branchID), uint(qrID)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
