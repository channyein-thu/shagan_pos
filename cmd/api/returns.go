package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"shagan_pos/internal/common"
	"shagan_pos/internal/returns"
)

type ReturnsAPI struct {
	service returns.Interface
}

func NewReturnsAPI(db *gorm.DB) *ReturnsAPI {
	return &ReturnsAPI{service: returns.NewService(returns.NewRepository(db))}
}

func (a *ReturnsAPI) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/sales/:id/void", a.VoidSale)
	rg.GET("/voids", a.ListVoids)
	rg.POST("/returns", a.CreateReturn)
	rg.GET("/returns", a.ListReturns)
	rg.GET("/returns/:id", a.GetReturn)
	rg.POST("/exchanges", a.CreateExchange)
	rg.GET("/exchanges", a.ListExchanges)
	rg.GET("/exchanges/:id", a.GetExchange)
}

// VoidSale handles `POST /sales/:id/void`. Full or partial
func (a *ReturnsAPI) VoidSale(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	var in returns.Void
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.VoidSale(c.Request.Context(), id, in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListVoids handles `GET /voids`.
func (a *ReturnsAPI) ListVoids(c *gin.Context) {
	result, err := a.service.ListVoids(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateReturn handles `POST /returns`. Also writes return_items
func (a *ReturnsAPI) CreateReturn(c *gin.Context) {
	var in returns.Return
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.CreateReturn(c.Request.Context(), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListReturns handles `GET /returns`.
func (a *ReturnsAPI) ListReturns(c *gin.Context) {
	result, err := a.service.ListReturns(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetReturn handles `GET /returns/:id`.
func (a *ReturnsAPI) GetReturn(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.GetReturn(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateExchange handles `POST /exchanges`. Also writes exchange_items
func (a *ReturnsAPI) CreateExchange(c *gin.Context) {
	var in returns.Exchange
	if err := c.ShouldBindJSON(&in); err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result, err := a.service.CreateExchange(c.Request.Context(), in)
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusCreated, result)
}

// ListExchanges handles `GET /exchanges`.
func (a *ReturnsAPI) ListExchanges(c *gin.Context) {
	result, err := a.service.ListExchanges(c.Request.Context())
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetExchange handles `GET /exchanges/:id`.
func (a *ReturnsAPI) GetExchange(c *gin.Context) {
	idVal, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.JSONError(c, http.StatusBadRequest, "invalid_id", "invalid id")
		return
	}
	result, err := a.service.GetExchange(c.Request.Context(), uint(idVal))
	if err != nil {
		common.JSONError(c, http.StatusNotImplemented, "not_implemented", err.Error())
		return
	}
	c.JSON(http.StatusOK, result)
}
