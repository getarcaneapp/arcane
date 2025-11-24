package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/services"
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

// Search delegates to the customize search service and returns relevance-scored results
func (h *CustomizeHandler) Search(c *gin.Context) {
	var req dto.CustomizeSearchRequest
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

// GetCategories returns all available customization categories with metadata
func (h *CustomizeHandler) GetCategories(c *gin.Context) {
	categories := h.customizeSearchService.GetCustomizeCategories()
	c.JSON(http.StatusOK, categories)
}
