package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/storage"
)

type CatalogAPI struct {
	service catalog.Interface
}

func NewCatalogAPI(db *gorm.DB, store storage.Storage) *CatalogAPI {
	return &CatalogAPI{service: catalog.NewService(catalog.NewRepository(db), identity.NewRepository(db), db, store)}
}

func (a *CatalogAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/products", a.ListProducts)
	rg.GET("/products/:id", a.GetProduct)
	rg.GET("/products/barcode/:code", a.GetProductByBarcode)
	rg.POST("/products", a.CreateProduct)
	rg.PATCH("/products/:id", a.UpdateProduct)
	rg.DELETE("/products/:id", a.DeleteProduct)
	rg.GET("/categories", a.ListCategories)
	rg.POST("/categories", a.CreateCategory)
	rg.PATCH("/categories/:id", a.UpdateCategory)
	rg.DELETE("/categories/:id", a.DeleteCategory)
	rg.POST("/media", a.UploadMedia)
	rg.GET("/combos", a.ListCombos)
	rg.POST("/combos", a.CreateCombo)
	rg.PATCH("/combos/:id", a.UpdateCombo)
	rg.DELETE("/combos/:id", a.DeleteCombo)
}

// ListProducts handles `GET /products`. Restricted to the caller's own
// branch when the caller's token carries one (a pos device) - org-wide for
// owner/service_center, same reasoning as identity.ListStaff.
func (a *CatalogAPI) ListProducts(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var branchID *uint
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = &bID
	}
	result, err := a.service.ListProducts(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProduct handles `GET /products/:id`.
func (a *CatalogAPI) GetProduct(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetProduct(c.Request.Context(), orgID, uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProductByBarcode handles `GET /products/barcode/:code`. Exact-match
// scan lookup, e.g. for a checkout scan. A barcode is only unique per branch
// (not per org), so a branch is always required to resolve it unambiguously:
// a pos-device token supplies its own branch automatically (same as
// ListProducts); an owner/service_center token must pass ?branch_id=.
func (a *CatalogAPI) GetProductByBarcode(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	var branchID uint
	if bID, ok := middleware.BranchIDFromContext(c); ok {
		branchID = bID
	} else {
		v, err := strconv.ParseUint(c.Query("branch_id"), 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("branch_id is required"))
			return
		}
		branchID = uint(v)
	}

	code := c.Param("code")
	result, err := a.service.GetProductByBarcode(c.Request.Context(), orgID, branchID, code)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateProduct handles `POST /products`. Multipart form: every product
// field as a form field (branch_id, category_id, name, barcode, price,
// discount, tax, threshold, is_active, and optional modifier) plus "image"
// (the required product photo) - a product can't be created without one, so
// this isn't a
// JSON endpoint like the other catalog creates.
func (a *CatalogAPI) CreateProduct(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	branchID, err := strconv.ParseUint(c.PostForm("branch_id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid branch_id"))
		return
	}
	categoryID, err := strconv.ParseUint(c.PostForm("category_id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid category_id"))
		return
	}
	name := c.PostForm("name")
	if name == "" {
		common.HandleError(c, common.BadRequestError("name is required"))
		return
	}
	barcode := c.PostForm("barcode")
	if barcode == "" {
		common.HandleError(c, common.BadRequestError("barcode is required"))
		return
	}
	price, err := decimal.NewFromString(c.PostForm("price"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid price"))
		return
	}
	discount, err := decimal.NewFromString(c.DefaultPostForm("discount", "0"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid discount"))
		return
	}
	tax, err := decimal.NewFromString(c.DefaultPostForm("tax", "0"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid tax"))
		return
	}
	threshold, err := strconv.Atoi(c.PostForm("threshold"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid threshold"))
		return
	}
	isActive, _ := strconv.ParseBool(c.DefaultPostForm("is_active", "false"))
	// modifier is optional and genuinely nullable - an omitted or empty form
	// field means nil, not an empty string.
	var modifier *string
	if v := c.PostForm("modifier"); v != "" {
		modifier = &v
	}

	fileHeader, err := c.FormFile("image")
	if err != nil {
		common.HandleError(c, common.BadRequestError("image is required"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.HandleError(c, common.BadRequestError("could not read image"))
		return
	}
	defer file.Close()

	in := catalog.CreateProductRequest{
		BranchID:   uint(branchID),
		CategoryID: uint(categoryID),
		Name:       name,
		Barcode:    barcode,
		Price:      price,
		Discount:   discount,
		Tax:        tax,
		Threshold:  threshold,
		IsActive:   isActive,
		Modifier:   modifier,
	}

	result, err := a.service.CreateProduct(
		c.Request.Context(),
		orgID,
		in,
		file,
		fileHeader.Size,
		fileHeader.Header.Get("Content-Type"),
		fileHeader.Filename,
	)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateProduct handles `PATCH /products/:id`. Multipart form: every field
// is optional (send only what changed), plus an optional "image" file that
// entirely replaces the product's existing photo - same reasoning as
// CreateProduct's required one, except here it's optional and this is a
// partial update. Uses GetPostForm (not PostForm) per field to tell
// "omitted" from "sent" apart, since a plain form field can't otherwise
// distinguish the two - see UpdateProductRequest's doc.
func (a *CatalogAPI) UpdateProduct(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}

	var in catalog.UpdateProductRequest
	if v, ok := c.GetPostForm("branch_id"); ok {
		branchID, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid branch_id"))
			return
		}
		bID := uint(branchID)
		in.BranchID = &bID
	}
	if v, ok := c.GetPostForm("category_id"); ok {
		categoryID, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid category_id"))
			return
		}
		cID := uint(categoryID)
		in.CategoryID = &cID
	}
	if v, ok := c.GetPostForm("name"); ok {
		in.Name = &v
	}
	if v, ok := c.GetPostForm("barcode"); ok {
		in.Barcode = &v
	}
	if v, ok := c.GetPostForm("price"); ok {
		price, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid price"))
			return
		}
		in.Price = &price
	}
	if v, ok := c.GetPostForm("discount"); ok {
		discount, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid discount"))
			return
		}
		in.Discount = &discount
	}
	if v, ok := c.GetPostForm("tax"); ok {
		tax, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid tax"))
			return
		}
		in.Tax = &tax
	}
	if v, ok := c.GetPostForm("threshold"); ok {
		threshold, err := strconv.Atoi(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid threshold"))
			return
		}
		in.Threshold = &threshold
	}
	if v, ok := c.GetPostForm("is_active"); ok {
		isActive, err := strconv.ParseBool(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid is_active"))
			return
		}
		in.IsActive = &isActive
	}
	if v, ok := c.GetPostForm("modifier"); ok {
		in.Modifier = &v
	}

	// image is optional - only read it when the caller actually attached one.
	var file io.ReadSeeker
	var fileSize int64
	var contentType, filename string
	if fileHeader, err := c.FormFile("image"); err == nil {
		f, err := fileHeader.Open()
		if err != nil {
			common.HandleError(c, common.BadRequestError("could not read image"))
			return
		}
		defer f.Close()
		file = f
		fileSize = fileHeader.Size
		contentType = fileHeader.Header.Get("Content-Type")
		filename = fileHeader.Filename
	}

	result, err := a.service.UpdateProduct(c.Request.Context(), orgID, uint(idVal), in, file, fileSize, contentType, filename)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteProduct handles `DELETE /products/:id`. Soft delete only
func (a *CatalogAPI) DeleteProduct(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteProduct(c.Request.Context(), uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListCategories handles `GET /categories`.
func (a *CatalogAPI) ListCategories(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListCategories(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCategory handles `POST /categories`.
func (a *CatalogAPI) CreateCategory(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	var in catalog.CreateCategoryRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCategory(c.Request.Context(), orgID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateCategory handles `PATCH /categories/:id`.
func (a *CatalogAPI) UpdateCategory(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in catalog.UpdateCategoryRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateCategory(c.Request.Context(), orgID, uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteCategory handles `DELETE /categories/:id`.
func (a *CatalogAPI) DeleteCategory(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteCategory(c.Request.Context(), orgID, uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// UploadMedia handles `POST /media`. Upload; returns storage_key
func (a *CatalogAPI) UploadMedia(c *gin.Context) {
	result, err := a.service.UploadMedia(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListCombos handles `GET /combos`.
func (a *CatalogAPI) ListCombos(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	result, err := a.service.ListCombos(c.Request.Context(), orgID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCombo handles `POST /combos`. Multipart form: name, price,
// expires_at, and items (a JSON-encoded array of {"product_id","qty"}
// objects) as form fields, plus an optional "image" file - unlike
// CreateProduct, a combo can exist without one.
func (a *CatalogAPI) CreateCombo(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}

	name := c.PostForm("name")
	if name == "" {
		common.HandleError(c, common.BadRequestError("name is required"))
		return
	}
	price, err := decimal.NewFromString(c.PostForm("price"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid price"))
		return
	}
	expiresAt, err := time.Parse(time.RFC3339, c.PostForm("expires_at"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid expires_at"))
		return
	}

	var items []catalog.CreateComboItemRequest
	if err := json.Unmarshal([]byte(c.PostForm("items")), &items); err != nil {
		common.HandleError(c, common.BadRequestError("invalid items"))
		return
	}
	if len(items) == 0 {
		common.HandleError(c, common.BadRequestError("items must have at least one entry"))
		return
	}
	for _, item := range items {
		if item.ProductID == 0 {
			common.HandleError(c, common.BadRequestError("product_id is required for every item"))
			return
		}
		if item.Qty < 1 {
			common.HandleError(c, common.BadRequestError("qty must be at least 1 for every item"))
			return
		}
	}

	in := catalog.CreateComboRequest{
		Name:      name,
		Price:     price,
		ExpiresAt: expiresAt,
		Items:     items,
	}

	// image is optional, unlike CreateProduct's - only read it when the
	// caller actually attached one.
	var file io.ReadSeeker
	var fileSize int64
	var contentType, filename string
	if fileHeader, err := c.FormFile("image"); err == nil {
		f, err := fileHeader.Open()
		if err != nil {
			common.HandleError(c, common.BadRequestError("could not read image"))
			return
		}
		defer f.Close()
		file = f
		fileSize = fileHeader.Size
		contentType = fileHeader.Header.Get("Content-Type")
		filename = fileHeader.Filename
	}

	result, err := a.service.CreateCombo(c.Request.Context(), orgID, in, file, fileSize, contentType, filename)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateCombo handles `PATCH /combos/:id`. Multipart form: name, price,
// expires_at (RFC3339) are all optional (send only what changed), plus an
// optional "image" file that entirely replaces the combo's existing photo -
// same reasoning as UpdateProduct's. Doesn't support editing items yet.
func (a *CatalogAPI) UpdateCombo(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}

	var in catalog.UpdateComboRequest
	if v, ok := c.GetPostForm("name"); ok {
		in.Name = &v
	}
	if v, ok := c.GetPostForm("price"); ok {
		price, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid price"))
			return
		}
		in.Price = &price
	}
	if v, ok := c.GetPostForm("expires_at"); ok {
		expiresAt, err := time.Parse(time.RFC3339, v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid expires_at"))
			return
		}
		in.ExpiresAt = &expiresAt
	}

	// image is optional - only read it when the caller actually attached one.
	var file io.ReadSeeker
	var fileSize int64
	var contentType, filename string
	if fileHeader, err := c.FormFile("image"); err == nil {
		f, err := fileHeader.Open()
		if err != nil {
			common.HandleError(c, common.BadRequestError("could not read image"))
			return
		}
		defer f.Close()
		file = f
		fileSize = fileHeader.Size
		contentType = fileHeader.Header.Get("Content-Type")
		filename = fileHeader.Filename
	}

	result, err := a.service.UpdateCombo(c.Request.Context(), orgID, uint(idVal), in, file, fileSize, contentType, filename)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteCombo handles `DELETE /combos/:id`.
func (a *CatalogAPI) DeleteCombo(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteCombo(c.Request.Context(), uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
