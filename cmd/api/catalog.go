package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"shagan_pos/internal/catalog"
	"shagan_pos/internal/common"
)

type CatalogAPI struct {
	service catalog.Interface
}

func NewCatalogAPI(db *gorm.DB) *CatalogAPI {
	return &CatalogAPI{service: catalog.NewService(catalog.NewRepository(db))}
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

// ListProducts handles `GET /products`. List/search/filter
func (a *CatalogAPI) ListProducts(c *gin.Context) {
	result, err := a.service.ListProducts(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProduct handles `GET /products/:id`.
func (a *CatalogAPI) GetProduct(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	result, err := a.service.GetProduct(c.Request.Context(), uint(idVal))
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetProductByBarcode handles `GET /products/barcode/:code`. Exact-match scan lookup
func (a *CatalogAPI) GetProductByBarcode(c *gin.Context) {
	code := c.Param("code")
	result, err := a.service.GetProductByBarcode(c.Request.Context(), code)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateProduct handles `POST /products`.
func (a *CatalogAPI) CreateProduct(c *gin.Context) {
	var in catalog.Product
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateProduct(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateProduct handles `PATCH /products/:id`.
func (a *CatalogAPI) UpdateProduct(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in catalog.Product
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateProduct(c.Request.Context(), uint(idVal), in)
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
	result, err := a.service.ListCategories(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCategory handles `POST /categories`.
func (a *CatalogAPI) CreateCategory(c *gin.Context) {
	var in catalog.Category
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCategory(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateCategory handles `PATCH /categories/:id`.
func (a *CatalogAPI) UpdateCategory(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in catalog.Category
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateCategory(c.Request.Context(), uint(idVal), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteCategory handles `DELETE /categories/:id`.
func (a *CatalogAPI) DeleteCategory(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	if err := a.service.DeleteCategory(c.Request.Context(), uint(idVal)); err != nil {
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
	result, err := a.service.ListCombos(c.Request.Context())
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateCombo handles `POST /combos`.
func (a *CatalogAPI) CreateCombo(c *gin.Context) {
	var in catalog.Combo
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.CreateCombo(c.Request.Context(), in)
	if err != nil {
		common.HandleError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// UpdateCombo handles `PATCH /combos/:id`.
func (a *CatalogAPI) UpdateCombo(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.HandleError(c, common.BadRequestError("invalid id"))
		return
	}
	var in catalog.Combo
	if err := c.ShouldBindJSON(&in); err != nil {
		common.HandleError(c, common.BadRequestError(err.Error()))
		return
	}
	result, err := a.service.UpdateCombo(c.Request.Context(), uint(idVal), in)
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
