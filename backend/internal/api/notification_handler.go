package api

import (
	"net/http"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/gin-gonic/gin"
	"go.getarcane.app/types/base"
	"go.getarcane.app/types/notification"
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

// GetAllSettings godoc
//
//	@Summary		Get all notification settings
//	@Description	Get all notification provider settings
//	@Tags			Notifications
//	@Param			id	path	string	true	"Environment ID"
//	@Success		200	{array}	notification.Response
//	@Router			/api/environments/{id}/notifications/settings [get]
func (h *NotificationHandler) GetAllSettings(c *gin.Context) {
	settings, err := h.notificationService.GetAllSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.NotificationSettingsListError{Err: err}).Error()})
		return
	}

	// Map to DTOs
	responses := make([]notification.Response, len(settings))
	for i, setting := range settings {
		responses[i] = notification.Response{
			ID:       setting.ID,
			Provider: notification.Provider(setting.Provider),
			Enabled:  setting.Enabled,
			Config:   base.JsonObject(setting.Config),
		}
	}

	c.JSON(http.StatusOK, responses)
}

// GetSettings godoc
//
//	@Summary		Get notification settings by provider
//	@Description	Get notification settings for a specific provider
//	@Tags			Notifications
//	@Param			id			path		string	true	"Environment ID"
//	@Param			provider	path		string	true	"Notification provider (discord, email)"
//	@Success		200			{object}	notification.Response
//	@Router			/api/environments/{id}/notifications/settings/{provider} [get]
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

	response := notification.Response{
		ID:       settings.ID,
		Provider: notification.Provider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   base.JsonObject(settings.Config),
	}

	c.JSON(http.StatusOK, response)
}

// CreateOrUpdateSettings godoc
//
//	@Summary		Create or update notification settings
//	@Description	Create or update notification settings for a provider
//	@Tags			Notifications
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string					true	"Environment ID"
//	@Param			settings	body		notification.Update		true	"Notification settings"
//	@Success		200			{object}	notification.Response
//	@Router			/api/environments/{id}/notifications/settings [post]
func (h *NotificationHandler) CreateOrUpdateSettings(c *gin.Context) {
	var req notification.Update
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	settings, err := h.notificationService.CreateOrUpdateSettings(
		c.Request.Context(),
		models.NotificationProvider(req.Provider),
		req.Enabled,
		models.JSON(req.Config),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.NotificationSettingsUpdateError{Err: err}).Error()})
		return
	}

	response := notification.Response{
		ID:       settings.ID,
		Provider: notification.Provider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   base.JsonObject(settings.Config),
	}

	c.JSON(http.StatusOK, response)
}

// DeleteSettings godoc
//
//	@Summary		Delete notification settings
//	@Description	Delete notification settings for a provider
//	@Tags			Notifications
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			provider	path		string	true	"Notification provider (discord, email)"
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/notifications/settings/{provider} [delete]
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

// TestNotification godoc
//
//	@Summary		Test notification
//	@Description	Send a test notification for a provider
//	@Tags			Notifications
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id			path		string	true	"Environment ID"
//	@Param			provider	path		string	true	"Notification provider (discord, email)"
//	@Param			type		query		string	false	"Test type (simple or image-update)"	default(simple)
//	@Success		200			{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		400			{object}	base.ApiResponse[base.ErrorResponse]
//	@Failure		500			{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/notifications/test/{provider} [post]
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

// GetAppriseSettings godoc
//
//	@Summary		Get Apprise settings
//	@Description	Get Apprise notification settings
//	@Tags			Notifications
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	notification.AppriseResponse
//	@Router			/api/environments/{id}/notifications/apprise [get]
func (h *NotificationHandler) GetAppriseSettings(c *gin.Context) {
	settings, err := h.appriseService.GetSettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": (&common.AppriseSettingsNotFoundError{}).Error()})
		return
	}

	response := notification.AppriseResponse{
		ID:                 settings.ID,
		APIURL:             settings.APIURL,
		Enabled:            settings.Enabled,
		ImageUpdateTag:     settings.ImageUpdateTag,
		ContainerUpdateTag: settings.ContainerUpdateTag,
	}

	c.JSON(http.StatusOK, response)
}

// CreateOrUpdateAppriseSettings godoc
//
//	@Summary		Create or update Apprise settings
//	@Description	Create or update Apprise notification settings
//	@Tags			Notifications
//	@Accept			json
//	@Produce		json
//	@Param			id			path		string						true	"Environment ID"
//	@Param			settings	body		notification.AppriseUpdate	true	"Apprise settings"
//	@Success		200			{object}	notification.AppriseResponse
//	@Router			/api/environments/{id}/notifications/apprise [post]
func (h *NotificationHandler) CreateOrUpdateAppriseSettings(c *gin.Context) {
	var req notification.AppriseUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": (&common.InvalidRequestFormatError{Err: err}).Error()})
		return
	}

	if req.Enabled && req.APIURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "API URL is required when Apprise is enabled"})
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

	response := notification.AppriseResponse{
		ID:                 settings.ID,
		APIURL:             settings.APIURL,
		Enabled:            settings.Enabled,
		ImageUpdateTag:     settings.ImageUpdateTag,
		ContainerUpdateTag: settings.ContainerUpdateTag,
	}

	c.JSON(http.StatusOK, response)
}

// TestAppriseNotification godoc
//
//	@Summary		Test Apprise notification
//	@Description	Send a test notification via Apprise
//	@Tags			Notifications
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Param			id	path		string	true	"Environment ID"
//	@Success		200	{object}	base.ApiResponse[base.MessageResponse]
//	@Failure		500	{object}	base.ApiResponse[base.ErrorResponse]
//	@Router			/api/environments/{id}/notifications/apprise/test [post]
func (h *NotificationHandler) TestAppriseNotification(c *gin.Context) {
	if err := h.appriseService.TestNotification(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": (&common.AppriseTestError{Err: err}).Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Test notification sent successfully"})
}
