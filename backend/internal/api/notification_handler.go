package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ofkm/arcane-backend/internal/common"
	"github.com/ofkm/arcane-backend/internal/dto"
	"github.com/ofkm/arcane-backend/internal/middleware"
	"github.com/ofkm/arcane-backend/internal/models"
	"github.com/ofkm/arcane-backend/internal/services"
)

type NotificationHandler struct {
	notificationService *services.NotificationService
	appriseService      *services.AppriseService
}

func NewNotificationHandler(group *gin.RouterGroup, notificationService *services.NotificationService, appriseService *services.AppriseService, authMiddleware *middleware.AuthMiddleware) {
	handler := &NotificationHandler{
		notificationService: notificationService,
		appriseService:      appriseService,
	}

	notifications := group.Group("/environments/:id/notifications")
	notifications.Use(authMiddleware.WithAdminRequired().Add())
	{
		notifications.GET("/settings", handler.GetAllSettings)
		notifications.GET("/settings/:provider", handler.GetSettings)
		notifications.POST("/settings", handler.CreateOrUpdateSettings)
		notifications.DELETE("/settings/:provider", handler.DeleteSettings)
		notifications.POST("/test/:provider", handler.TestNotification)

		notifications.GET("/apprise", handler.GetAppriseSettings)
		notifications.POST("/apprise", handler.CreateOrUpdateAppriseSettings)
		notifications.POST("/apprise/test", handler.TestAppriseNotification)
	}
}

func (h *NotificationHandler) GetAllSettings(c *gin.Context) {
	settings, err := h.notificationService.GetAllSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.NotificationSettingsListError{Err: err}).Error()})
		return
	}

	// Map to DTOs
	responses := make([]dto.NotificationSettingsResponse, len(settings))
	for i, setting := range settings {
		responses[i] = dto.NotificationSettingsResponse{
			ID:       setting.ID,
			Provider: setting.Provider,
			Enabled:  setting.Enabled,
			Config:   setting.Config,
		}
	}

	c.JSON(http.StatusOK, responses)
}

func (h *NotificationHandler) GetSettings(c *gin.Context) {
	providerStr := c.Param("provider")
	provider := models.NotificationProvider(providerStr)

	switch provider {
	case models.NotificationProviderDiscord, models.NotificationProviderEmail:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidNotificationProviderError{}).Error()})
		return
	}

	settings, err := h.notificationService.GetSettingsByProvider(c.Request.Context(), provider)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": (&common.NotificationSettingsNotFoundError{}).Error()})
		return
	}

	response := dto.NotificationSettingsResponse{
		ID:       settings.ID,
		Provider: settings.Provider,
		Enabled:  settings.Enabled,
		Config:   settings.Config,
	}

	c.JSON(http.StatusOK, response)
}

func (h *NotificationHandler) CreateOrUpdateSettings(c *gin.Context) {
	var req dto.NotificationSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	settings, err := h.notificationService.CreateOrUpdateSettings(
		c.Request.Context(),
		req.Provider,
		req.Enabled,
		req.Config,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.NotificationSettingsUpdateError{Err: err}).Error()})
		return
	}

	response := dto.NotificationSettingsResponse{
		ID:       settings.ID,
		Provider: settings.Provider,
		Enabled:  settings.Enabled,
		Config:   settings.Config,
	}

	c.JSON(http.StatusOK, response)
}

func (h *NotificationHandler) DeleteSettings(c *gin.Context) {
	providerStr := c.Param("provider")
	provider := models.NotificationProvider(providerStr)

	switch provider {
	case models.NotificationProviderDiscord, models.NotificationProviderEmail:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidNotificationProviderError{}).Error()})
		return
	}

	if err := h.notificationService.DeleteSettings(c.Request.Context(), provider); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.NotificationSettingsDeletionError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings deleted successfully"})
}

func (h *NotificationHandler) TestNotification(c *gin.Context) {
	providerStr := c.Param("provider")
	provider := models.NotificationProvider(providerStr)

	switch provider {
	case models.NotificationProviderDiscord, models.NotificationProviderEmail:
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidNotificationProviderError{}).Error()})
		return
	}

	testType := c.DefaultQuery("type", "simple") // "simple" or "image-update"

	if err := h.notificationService.TestNotification(c.Request.Context(), provider, testType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.NotificationTestError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test notification sent successfully"})
}

func (h *NotificationHandler) GetAppriseSettings(c *gin.Context) {
	settings, err := h.appriseService.GetSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": (&common.AppriseSettingsNotFoundError{}).Error()})
		return
	}

	response := dto.AppriseSettingsResponse{
		ID:                 settings.ID,
		APIURL:             settings.APIURL,
		Enabled:            settings.Enabled,
		ImageUpdateTag:     settings.ImageUpdateTag,
		ContainerUpdateTag: settings.ContainerUpdateTag,
	}

	c.JSON(http.StatusOK, response)
}

func (h *NotificationHandler) CreateOrUpdateAppriseSettings(c *gin.Context) {
	var req dto.AppriseSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	settings, err := h.appriseService.CreateOrUpdateSettings(
		c.Request.Context(),
		req.APIURL,
		req.Enabled,
		req.ImageUpdateTag,
		req.ContainerUpdateTag,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.AppriseSettingsUpdateError{Err: err}).Error()})
		return
	}

	response := dto.AppriseSettingsResponse{
		ID:                 settings.ID,
		APIURL:             settings.APIURL,
		Enabled:            settings.Enabled,
		ImageUpdateTag:     settings.ImageUpdateTag,
		ContainerUpdateTag: settings.ContainerUpdateTag,
	}

	c.JSON(http.StatusOK, response)
}

func (h *NotificationHandler) TestAppriseNotification(c *gin.Context) {
	if err := h.appriseService.TestNotification(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.AppriseTestError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test notification sent successfully"})
}
