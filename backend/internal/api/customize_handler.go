package api

import (
	"net/http"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/search"
)

type CustomizeHandler struct {
	customizeSearchService *services.CustomizeSearchService
}

func NewCustomizeHandler(group *gin.RouterGroup, customizeSearchService *services.CustomizeSearchService, authMiddleware *middleware.AuthMiddleware) {
	handler := &CustomizeHandler{
		customizeSearchService: customizeSearchService,
	}

	// Expose customize search and categories endpoints under /api/customize
	apiGroup := group.Group("/customize")
	apiGroup.POST("/search", authMiddleware.WithAdminNotRequired().Add(), handler.Search)
	apiGroup.GET("/categories", authMiddleware.WithAdminNotRequired().Add(), handler.GetCategories)
}

// Search godoc
//
//	@Summary		Search customization options
//	@Description	Search customization categories and options by query
//	@Tags			Customize
//	@Accept			json
//	@Produce		json
//	@Param			request	body		search.Request	true	"Search query"
//	@Success		200		{object}	search.Response
//	@Router			/api/customize/search [post]
//
// Search delegates to the customize search service and returns relevance-scored results
func (h *CustomizeHandler) Search(c *gin.Context) {
	var req search.Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.QueryParameterRequiredError{}).Error()},
		})
		return
	}

	results := h.customizeSearchService.Search(req.Query)
	c.JSON(http.StatusOK, results)
}

// GetCategories godoc
//
//	@Summary		Get customization categories
//	@Description	Get all available customization categories with metadata
//	@Tags			Customize
//	@Success		200	{array}	search.Category
//	@Router			/api/customize/categories [get]
//
// GetCategories returns all available customization categories with metadata
func (h *CustomizeHandler) GetCategories(c *gin.Context) {
	categories := h.customizeSearchService.GetCustomizeCategories()
	c.JSON(http.StatusOK, categories)
}
