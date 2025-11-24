package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/config"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/services"
)

type SettingsHandler struct {
	settingsService       *services.SettingsService
	settingsSearchService *services.SettingsSearchService
}

func NewSettingsHandler(group *gin.RouterGroup, settingsService *services.SettingsService, settingsSearchService *services.SettingsSearchService, authMiddleware *middleware.AuthMiddleware) {
	handler := &SettingsHandler{
		settingsService:       settingsService,
		settingsSearchService: settingsSearchService,
	}

	apiGroup := group.Group("/environments/:id/settings")

	apiGroup.GET("/public", handler.GetPublicSettings)
	apiGroup.GET("", authMiddleware.WithAdminNotRequired().Add(), handler.GetSettings)
	apiGroup.PUT("", authMiddleware.WithAdminRequired().Add(), handler.UpdateSettings)

	// Also expose top-level settings search and categories endpoints under /api/settings
	top := group.Group("/settings")
	top.POST("/search", authMiddleware.WithAdminNotRequired().Add(), handler.Search)
	top.GET("/categories", authMiddleware.WithAdminNotRequired().Add(), handler.GetCategories)
}

// Search delegates to the settings search service and returns relevance-scored results
func (h *SettingsHandler) Search(c *gin.Context) {
	var req dto.SettingsSearchRequest
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

	results := h.settingsSearchService.Search(req.Query)
	c.JSON(http.StatusOK, results)
}

// GetCategories returns all available settings categories with metadata
func (h *SettingsHandler) GetCategories(c *gin.Context) {
	categories := h.settingsSearchService.GetSettingsCategories()
	c.JSON(http.StatusOK, categories)
}

func (h *SettingsHandler) GetSettings(c *gin.Context) {
	environmentID := c.Param("id")

	showAll := environmentID == "0"
	settings := h.settingsService.ListSettings(showAll)

	var settingsDto []dto.PublicSettingDto
	if err := dto.MapStructList(settings, &settingsDto); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.SettingsMappingError{Err: err}).Error()},
		})
		return
	}

	settingsDto = append(settingsDto, dto.PublicSettingDto{
		Key:   "uiConfigDisabled",
		Value: strconv.FormatBool(config.Load().UIConfigurationDisabled),
		Type:  "boolean",
	})

	c.JSON(http.StatusOK, settingsDto)
}

func (h *SettingsHandler) GetPublicSettings(c *gin.Context) {
	settings := h.settingsService.ListSettings(false)

	var settingsDto []dto.PublicSettingDto
	if err := dto.MapStructList(settings, &settingsDto); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.SettingsMappingError{Err: err}).Error()},
		})
		return
	}

	settingsDto = append(settingsDto, dto.PublicSettingDto{
		Key:   "uiConfigDisabled",
		Value: strconv.FormatBool(config.Load().UIConfigurationDisabled),
		Type:  "boolean",
	})

	c.JSON(http.StatusOK, settingsDto)
}

func (h *SettingsHandler) UpdateSettings(c *gin.Context) {
	environmentID := c.Param("id")

	var req dto.UpdateSettingsDto
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()},
		})
		return
	}

	if environmentID != "0" {
		if req.AuthLocalEnabled != nil || req.AuthOidcEnabled != nil ||
			req.AuthSessionTimeout != nil || req.AuthPasswordPolicy != nil ||
			req.AuthOidcConfig != nil {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"data":    gin.H{"error": (&common.AuthSettingsUpdateError{}).Error()},
			})
			return
		}
	}

	updatedSettings, err := h.settingsService.UpdateSettings(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"data":    gin.H{"error": (&common.SettingsUpdateError{Err: err}).Error()},
		})
		return
	}

	settingDtos := make([]dto.SettingDto, 0, len(updatedSettings))
	for _, setting := range updatedSettings {
		settingDtos = append(settingDtos, dto.SettingDto{
			PublicSettingDto: dto.PublicSettingDto{
				Key:   setting.Key,
				Type:  "string",
				Value: setting.Value,
			},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"settings": settingDtos,
	})
}
