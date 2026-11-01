package notification

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/notifications"
	"getarcane.dev/types/v3"
)

type (
	NotificationHandler struct {
		notificationService *NotificationService
		config              *config.Config
	}

	GetAllNotificationSettingsOutput struct {
		Body []types.NotificationSettings
	}

	GetNotificationSettingsInput struct {
		Provider string `path:"provider" doc:"Provider"`
	}

	GetNotificationSettingsOutput struct {
		Body types.NotificationSettings
	}

	CreateOrUpdateNotificationSettingsInput struct {
		types.Create[types.NotificationSettings]
	}

	CreateOrUpdateNotificationSettingsOutput struct {
		Body types.NotificationSettings
	}

	DeleteNotificationSettingsInput struct {
		Provider string `path:"provider" doc:"Provider"`
	}

	NotificationSettingsActionInput struct {
		Provider string `path:"provider" doc:"Provider"`
		Action   string `query:"action" required:"true" enum:"test" doc:"Notification settings action"`
		Type     string `query:"type" default:"simple"`
	}

	DispatchNotificationInput struct {
		Body   types.NotificationDispatchRequest
		APIKey string `header:"X-API-Key" doc:"Remote environment access token"`
	}
)

func normalizeNotificationTestType(testType string) string {
	normalized := strings.TrimSpace(testType)
	return kit.Ternary(normalized == "", notificationTestTypeSimple, normalized)
}

func isSupportedNotificationTestType(testType string) bool {
	_, ok := notificationTestEventTypes[testType]
	return ok
}

func (h *NotificationHandler) rejectOnAgent() error {
	if h.config != nil && h.config.AgentEnabled {
		return huma.Error400BadRequest("notifications are managed on the Arcane manager")
	}
	return nil
}

func (h *NotificationHandler) GetAllNotificationSettings(ctx context.Context, _ *struct{}) (*GetAllNotificationSettingsOutput, error) {
	if err := h.rejectOnAgent(); err != nil {
		return nil, err
	}
	settings, err := h.notificationService.GetAllSettings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list notification settings: " + err.Error())
	}

	responses := make([]types.NotificationSettings, len(settings))
	for i, setting := range settings {
		responses[i] = types.NotificationSettings{
			ID:       setting.ID,
			Provider: types.NotificationProvider(setting.Provider),
			Enabled:  setting.Enabled,
			Config:   types.JsonObject(RedactNotificationConfigCredentials(setting.Provider, setting.Config)),
		}
	}

	return &GetAllNotificationSettingsOutput{Body: responses}, nil
}

func (h *NotificationHandler) GetNotificationSettings(ctx context.Context, input *GetNotificationSettingsInput) (*GetNotificationSettingsOutput, error) {
	if err := h.rejectOnAgent(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Provider)

	settings, err := h.notificationService.GetSettingsByProvider(ctx, provider)
	if err != nil {
		return nil, huma.Error404NotFound("Settings not found")
	}

	response := types.NotificationSettings{
		ID:       settings.ID,
		Provider: types.NotificationProvider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   types.JsonObject(RedactNotificationConfigCredentials(settings.Provider, settings.Config)),
	}

	return &GetNotificationSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) CreateOrUpdateNotificationSettings(ctx context.Context, input *CreateOrUpdateNotificationSettingsInput) (*CreateOrUpdateNotificationSettingsOutput, error) {
	if err := h.rejectOnAgent(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Body.Provider)
	if !notifications.IsValidNotificationProvider(provider) {
		return nil, huma.Error400BadRequest("invalid provider")
	}

	settings, err := h.notificationService.CreateOrUpdateSettings(
		ctx,
		provider,
		input.Body.Enabled,
		database.JSON(input.Body.Config),
	)
	if err != nil {
		apiErr := common.ToAPIError(err)
		if apiErr.HTTPStatus() == http.StatusInternalServerError {
			return nil, huma.Error500InternalServerError("Failed to update notification settings")
		}
		return nil, handlerutil.APIError(err)
	}

	response := types.NotificationSettings{
		ID:       settings.ID,
		Provider: types.NotificationProvider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   types.JsonObject(RedactNotificationConfigCredentials(settings.Provider, settings.Config)),
	}

	return &CreateOrUpdateNotificationSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) DeleteNotificationSettings(ctx context.Context, input *DeleteNotificationSettingsInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.rejectOnAgent(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Provider)

	if err := h.notificationService.DeleteSettings(ctx, provider); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete notification settings: " + err.Error())
	}

	return handlerutil.MessageOutput("Settings deleted successfully", ""), nil
}

func (h *NotificationHandler) TestNotification(ctx context.Context, input *NotificationSettingsActionInput) (*handlerutil.Out[types.NotificationTestResponse], error) {
	if err := h.rejectOnAgent(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Provider)
	testType := normalizeNotificationTestType(input.Type)
	if !isSupportedNotificationTestType(testType) {
		return nil, huma.Error400BadRequest("invalid notification test type")
	}

	warning, err := h.notificationService.TestNotification(ctx, provider, testType)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to send test notification: "+apiErr.Message)
	}

	return &handlerutil.Out[types.NotificationTestResponse]{
		Body: types.ApiResponse[types.NotificationTestResponse]{
			Success: true,
			Data: types.NotificationTestResponse{
				Message: "Test notification sent successfully",
				Warning: warning,
			},
		},
	}, nil
}

func (h *NotificationHandler) DispatchNotification(ctx context.Context, input *DispatchNotificationInput) (*handlerutil.Out[types.NotificationDispatchResponse], error) {
	if err := h.rejectOnAgent(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		return nil, huma.Error401Unauthorized("missing remote environment access token")
	}
	dispatchResponse, err := h.notificationService.DispatchNotification(ctx, input.APIKey, input.Body)
	if err != nil {
		if errors.Is(err, ErrUnsupportedDispatchKind) {
			return nil, huma.Error400BadRequest("unsupported dispatch kind")
		}
		if errors.Is(err, ErrUnauthorizedNotificationDispatch) {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		if errors.Is(err, common.ErrValidation) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError("dispatch failed")
	}

	return &handlerutil.Out[types.NotificationDispatchResponse]{
		Body: types.ApiResponse[types.NotificationDispatchResponse]{
			Success: true,
			Data:    dispatchResponse,
		},
	}, nil
}
