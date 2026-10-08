package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/platform"
	"shagan_pos/internal/storage"
)

type PlatformAPI struct {
	service   platform.Interface
	jwtSecret []byte
}

func NewPlatformAPI(db *gorm.DB, store storage.Storage, jwtSecret []byte) *PlatformAPI {
	return &PlatformAPI{service: platform.NewService(platform.NewRepository(db), identity.NewRepository(db), db, store), jwtSecret: jwtSecret}
}

func (a *PlatformAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// Settings/QR writes need Back Office (owner/service_center, or a
	// manager's staff token at the till); reads stay open - the till prints
	// receipts and shows payment QRs to customers.
	backOffice := middleware.RequireBackOffice(a.jwtSecret)
	rg.GET("/receipt-settings", a.GetReceiptSettings)
	rg.PUT("/receipt-settings", backOffice, a.UpdateReceiptSettings)
	rg.POST("/printers/test", a.TestPrinter)
	rg.POST("/branches/:id/payment-qr-codes", backOffice, a.UploadPaymentQRCode)
	rg.GET("/branches/:id/payment-qr-codes", a.ListPaymentQRCodes)
	rg.DELETE("/branches/:id/payment-qr-codes/:qrId", backOffice, a.DeletePaymentQRCode)
}

// scopedBranchID resolves the branch a request targets: a pos-device/manager
// token (carries its own BranchID) is restricted to that branch - requested
// is only honored if it matches; an owner/service_center token (org-wide)
// may target any branch id it supplies.
func scopedBranchID(c *gin.Context, requested uint) (uint, bool) {
	if ownBranchID, ok := middleware.BranchIDFromContext(c); ok {
		if requested != ownBranchID {
			common.HandleError(c, common.ForbiddenError("cannot act on a different branch"))
			return 0, false
		}
		return ownBranchID, true
	}
	return requested, true
}

// GetReceiptSettings handles `GET /receipt-settings`. branch_id is optional -
// a pos-device/manager token defaults to its own branch; an owner/
// service_center token gets the org-wide default when it's omitted.
func (a *PlatformAPI) GetReceiptSettings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	var branchID *uint
	if ownBranchID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = &ownBranchID
	} else if v := c.Query("branch_id"); v != "" {
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid branch_id"))
			return
		}
		id := uint(parsed)
		branchID = &id
	}

	result, err := a.service.GetReceiptSettings(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UpdateReceiptSettings handles `PUT /receipt-settings`.
func (a *PlatformAPI) UpdateReceiptSettings(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	var in platform.UpdateReceiptSettingsRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}

	if in.BranchID != nil {
		branchID, ok := scopedBranchID(c, *in.BranchID)
		if !ok {
			return
		}
		in.BranchID = &branchID
	} else if ownBranchID, ok := middleware.BranchIDFromContext(c); ok {
		in.BranchID = &ownBranchID
	}

	result, err := a.service.UpdateReceiptSettings(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// TestPrinter handles `POST /printers/test`. branch_id is required (query
// param) since a printer is physically at one branch.
func (a *PlatformAPI) TestPrinter(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	var requested uint
	if ownBranchID, ok := middleware.BranchIDFromContext(c); ok {
		requested = ownBranchID
	} else {
		v, err := strconv.ParseUint(c.Query("branch_id"), 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("branch_id is required"))
			return
		}
		requested = uint(v)
	}

	branchID, ok := scopedBranchID(c, requested)
	if !ok {
		return
	}

	result, err := a.service.TestPrinter(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// UploadPaymentQRCode handles `POST /branches/:id/payment-qr-codes`.
// Multipart form: "file" (the QR image) + "bank_name".
func (a *PlatformAPI) UploadPaymentQRCode(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	pathBranchID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	branchID, ok := scopedBranchID(c, uint(pathBranchID))
	if !ok {
		return
	}

	bankName := c.PostForm("bank_name")
	if bankName == "" {
		common.HandleError(c, common.BadRequestError("bank_name is required"))
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
		orgID,
		branchID,
		bankName,
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
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	pathBranchID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	branchID, ok := scopedBranchID(c, uint(pathBranchID))
	if !ok {
		return
	}

	result, err := a.service.ListPaymentQRCodes(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeletePaymentQRCode handles `DELETE /branches/:id/payment-qr-codes/:qrId`.
func (a *PlatformAPI) DeletePaymentQRCode(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	pathBranchID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	branchID, ok := scopedBranchID(c, uint(pathBranchID))
	if !ok {
		return
	}
	qrID, err := strconv.ParseUint(c.Param("qrId"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid qrId"))
		return
	}

	if err := a.service.DeletePaymentQRCode(c.Request.Context(), orgID, branchID, uint(qrID)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
