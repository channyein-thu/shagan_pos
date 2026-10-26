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

	"shagan_pos/internal/audit"
	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/storage"
)

type CatalogAPI struct {
	service   catalog.Interface
	jwtSecret []byte
}

func NewCatalogAPI(db *gorm.DB, store storage.Storage, jwtSecret []byte) *CatalogAPI {
	return &CatalogAPI{service: catalog.NewService(catalog.NewRepository(db), identity.NewRepository(db), audit.NewRepository(db), db, store), jwtSecret: jwtSecret}
}

func (a *CatalogAPI) RegisterRoutes(rg *gin.RouterGroup) {
	// Writes need Back Office with the manage_catalog permission: an
	// owner/service_center token, or a till's POS token plus an X-Staff-Token
	// carrying access_backoffice and manage_catalog. Reads stay open to any
	// authenticated caller - the till needs them to sell.
	backOffice := middleware.RequireBackOfficePermission(a.jwtSecret, "manage_catalog")
	rg.GET("/products", a.ListProducts)
	rg.GET("/products/:id", a.GetProduct)
	rg.GET("/products/barcode/:code", a.GetProductByBarcode)
	rg.POST("/products", backOffice, a.CreateProduct)
	rg.PATCH("/products/:id", backOffice, a.UpdateProduct)
	rg.DELETE("/products/:id", backOffice, a.DeleteProduct)
	// Branch price overrides. Reading them takes Back Office with manage_catalog
	// (an owner sees every branch; a till's manager only their own branch).
	// Setting or clearing one takes its own permission, manage_branch_prices:
	// an Owner / Service Center may write any branch of the org, a manager at a
	// till only their own (checked in the service - a 403 for another branch).
	branchPrices := middleware.RequireBackOfficePermission(a.jwtSecret, "manage_branch_prices")
	rg.GET("/products/:id/branch-prices", backOffice, a.ListBranchPrices)
	rg.PUT("/products/:id/branch-prices/:branchId", branchPrices, a.SetBranchPrice)
	rg.DELETE("/products/:id/branch-prices/:branchId", branchPrices, a.DeleteBranchPrice)
	rg.GET("/categories", a.ListCategories)
	rg.POST("/categories", backOffice, a.CreateCategory)
	rg.PATCH("/categories/:id", backOffice, a.UpdateCategory)
	rg.DELETE("/categories/:id", backOffice, a.DeleteCategory)
	rg.GET("/combos", a.ListCombos)
	rg.POST("/combos", backOffice, a.CreateCombo)
	rg.PATCH("/combos/:id", backOffice, a.UpdateCombo)
	rg.DELETE("/combos/:id", backOffice, a.DeleteCombo)
}

// ListProducts handles `GET /products`. The same catalog for every caller in
// the org, priced for a branch: a POS token gets its own branch's effective
// prices, an owner / service_center may pass ?branch_id= (without one they get
// the plain catalog prices). A product resolved for a branch carries
// "price_source": "branch" | "catalog".
func (a *CatalogAPI) ListProducts(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListProducts(c.Request.Context(), orgID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProduct handles `GET /products/:id`. Branch pricing as ListProducts.
func (a *CatalogAPI) GetProduct(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetProduct(c.Request.Context(), orgID, uint(idVal), branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProductByBarcode handles `GET /products/barcode/:code`. Exact-match
// scan lookup, e.g. for a checkout scan. Barcode is unique per org, so this
// resolves unambiguously without needing a branch; the price it returns is the
// branch's, as ListProducts.
func (a *CatalogAPI) GetProductByBarcode(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}

	code := c.Param("code")
	result, err := a.service.GetProductByBarcode(c.Request.Context(), orgID, code, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateProduct handles `POST /products`. Multipart form: every product
// field as a form field (category_id, name, barcode, price, discount, tax,
// threshold, is_active, and optional modifier) plus "image" (the required
// product photo) - a product can't be created without one, so this isn't a
// JSON endpoint like the other catalog creates.
func (a *CatalogAPI) CreateProduct(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
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
	// discount_percent is the only discount input; a client-sent `discount`
	// amount is ignored - the server computes it from the percent.
	discountPercent, err := decimal.NewFromString(c.DefaultPostForm("discount_percent", "0"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid discount_percent"))
		return
	}
	// tax_percent is the only tax input (the tax is inside the price); a
	// client-sent `tax` amount is ignored - the server computes it.
	taxPercent, err := decimal.NewFromString(c.DefaultPostForm("tax_percent", "0"))
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid tax_percent"))
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
		CategoryID:      uint(categoryID),
		Name:            name,
		Barcode:         barcode,
		Price:           price,
		DiscountPercent: discountPercent,
		TaxPercent:      taxPercent,
		Threshold:       threshold,
		IsActive:        isActive,
		Modifier:        modifier,
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
	if v, ok := c.GetPostForm("discount_percent"); ok {
		discountPercent, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid discount_percent"))
			return
		}
		in.DiscountPercent = &discountPercent
	}
	if v, ok := c.GetPostForm("tax_percent"); ok {
		taxPercent, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid tax_percent"))
			return
		}
		in.TaxPercent = &taxPercent
	}
	if v, ok := c.GetPostForm("cost_price"); ok {
		costPrice, err := decimal.NewFromString(v)
		if err != nil {
			common.HandleError(c, common.BadRequestError("invalid cost_price"))
			return
		}
		in.CostPrice = &costPrice
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

// DeleteProduct handles `DELETE /products/:id`. Hard delete, blocked if any
// combo still references the product.
func (a *CatalogAPI) DeleteProduct(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteProduct(c.Request.Context(), orgID, uint(idVal)); err != nil {
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

// DeleteCombo handles `DELETE /combos/:id`. Hard delete - also removes the
// combo's own items and image.
func (a *CatalogAPI) DeleteCombo(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteCombo(c.Request.Context(), orgID, uint(idVal)); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListBranchPrices handles `GET /products/:id/branch-prices`: one row per
// branch with its override (null = inherits the catalog value) and effective
// prices. A POS token (a manager in Back Office at a till) only ever sees its
// own branch; an owner / service_center sees every branch, or just
// ?branch_id=.
func (a *CatalogAPI) ListBranchPrices(c *gin.Context) {
	orgID, ok := requireOrgID(c)
	if !ok {
		return
	}
	productID, ok := pathID(c, "id")
	if !ok {
		return
	}
	branchID, ok := reportBranchID(c)
	if !ok {
		return
	}
	result, err := a.service.ListBranchPrices(c.Request.Context(), orgID, productID, branchID)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// SetBranchPrice handles `PUT /products/:id/branch-prices/:branchId`: upsert
// the branch's override. Body {"price": "1900.00", "discount_percent": null} -
// a null or omitted field inherits the catalog value; both null clears the
// override. Owner / service_center (any branch of the org) or a manager at a
// till with manage_branch_prices (their own branch only).
func (a *CatalogAPI) SetBranchPrice(c *gin.Context) {
	orgID, actor, productID, branchID, ok := branchPriceParams(c)
	if !ok {
		return
	}
	var in catalog.SetBranchPriceRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.SetBranchPrice(c.Request.Context(), orgID, actor, productID, branchID, in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteBranchPrice handles `DELETE /products/:id/branch-prices/:branchId`:
// the branch goes back to the catalog price. Idempotent. Same callers as
// SetBranchPrice.
func (a *CatalogAPI) DeleteBranchPrice(c *gin.Context) {
	orgID, actor, productID, branchID, ok := branchPriceParams(c)
	if !ok {
		return
	}
	if err := a.service.DeleteBranchPrice(c.Request.Context(), orgID, actor, productID, branchID); err != nil {
		common.HandleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// branchPriceParams reads what the two branch-price write routes share: the
// org, who is writing and the product and branch from the path. A POS token
// (a manager at a till) is the staff member from the X-Staff-Token the guard
// already verified, bound to the token's branch; an org-wide token is the user.
func branchPriceParams(c *gin.Context) (orgID uint, actor catalog.BranchPriceActor, productID, branchID uint, ok bool) {
	if orgID, ok = requireOrgID(c); !ok {
		return
	}
	if ownBranch, bound := middleware.BranchIDFromContext(c); bound {
		staffID, hasStaff := middleware.StaffIDFromContext(c)
		if !hasStaff {
			ok = false
			common.HandleError(c, common.UnauthorizedError("a staff token is required"))
			return
		}
		actor = catalog.BranchPriceActor{StaffID: &staffID, BranchID: &ownBranch}
	} else {
		userID, hasUser := middleware.UserIDFromContext(c)
		if !hasUser {
			ok = false
			common.HandleError(c, common.UnauthorizedError("missing or malformed authorization header"))
			return
		}
		actor = catalog.BranchPriceActor{UserID: &userID}
	}
	if productID, ok = pathID(c, "id"); !ok {
		return
	}
	branchID, ok = pathID(c, "branchId")
	return
}

// pathID parses a positive uint path parameter, or answers 400.
func pathID(c *gin.Context, name string) (uint, bool) {
	v, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || v == 0 {
		common.HandleError(c, common.BadRequestError("invalid "+name))
		return 0, false
	}
	return uint(v), true
}
