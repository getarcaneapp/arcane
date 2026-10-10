package notification

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/notification"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/notifications"
)

type NotificationHandler struct {
	notificationService *NotificationService
	config              *config.Config
}

type GetAllNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetAllNotificationSettingsOutput struct {
	Body []notification.Response
}

type GetNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
}

type GetNotificationSettingsOutput struct {
	Body notification.Response
}

type CreateOrUpdateNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          notification.Update
}

type CreateOrUpdateNotificationSettingsOutput struct {
	Body notification.Response
}

type DeleteNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
}

type TestNotificationInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
	Type          string `query:"type" default:"simple"`
}

type DispatchNotificationInput struct {
	APIKey string `header:"X-API-Key" doc:"Remote environment access token"`
	Body   notification.DispatchRequest
}

func normalizeNotificationTestType(testType string) string {
	normalized := strings.TrimSpace(testType)
	return kit.Ternary(normalized == "", notificationTestTypeSimple, normalized)
}

func isSupportedNotificationTestType(testType string) bool {
	_, ok := notificationTestEventTypes[testType]
	return ok
}

func (h *NotificationHandler) rejectIfAgentModeInternal() error {
	if h.config != nil && h.config.AgentMode {
		return huma.Error400BadRequest("notifications are managed on the Arcane manager")
	}
	return nil
}

func (h *NotificationHandler) GetAllNotificationSettings(ctx context.Context, input *GetAllNotificationSettingsInput) (*GetAllNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	settings, err := h.notificationService.GetAllSettings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list notification settings: " + err.Error())
	}

	responses := make([]notification.Response, len(settings))
	for i, setting := range settings {
		responses[i] = notification.Response{
			ID:       setting.ID,
			Provider: notification.Provider(setting.Provider),
			Enabled:  setting.Enabled,
			Config:   base.JsonObject(RedactNotificationConfigCredentials(setting.Provider, setting.Config)),
		}
	}

	return &GetAllNotificationSettingsOutput{Body: responses}, nil
}

func (h *NotificationHandler) GetNotificationSettings(ctx context.Context, input *GetNotificationSettingsInput) (*GetNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Provider)

	settings, err := h.notificationService.GetSettingsByProvider(ctx, provider)
	if err != nil {
		return nil, huma.Error404NotFound("Settings not found")
	}

	response := notification.Response{
		ID:       settings.ID,
		Provider: notification.Provider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   base.JsonObject(RedactNotificationConfigCredentials(settings.Provider, settings.Config)),
	}

	return &GetNotificationSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) CreateOrUpdateNotificationSettings(ctx context.Context, input *CreateOrUpdateNotificationSettingsInput) (*CreateOrUpdateNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
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
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	response := notification.Response{
		ID:       settings.ID,
		Provider: notification.Provider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   base.JsonObject(RedactNotificationConfigCredentials(settings.Provider, settings.Config)),
	}

	return &CreateOrUpdateNotificationSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) DeleteNotificationSettings(ctx context.Context, input *DeleteNotificationSettingsInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Provider)

	if err := h.notificationService.DeleteSettings(ctx, provider); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete notification settings: " + err.Error())
	}

	return handlerutil.MessageOutput("Settings deleted successfully", ""), nil
}

func (h *NotificationHandler) TestNotification(ctx context.Context, input *TestNotificationInput) (*handlerutil.Out[notification.TestResponse], error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := notifications.NotificationProvider(input.Provider)
	testType := normalizeNotificationTestType(input.Type)
	if !isSupportedNotificationTestType(testType) {
		return nil, huma.Error400BadRequest("invalid notification test type")
	}

	warning, err := h.notificationService.TestNotification(ctx, input.EnvironmentID, provider, testType)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to send test notification: " + err.Error())
	}

	return &handlerutil.Out[notification.TestResponse]{
		Body: base.ApiResponse[notification.TestResponse]{
			Success: true,
			Data: notification.TestResponse{
				Message: "Test notification sent successfully",
				Warning: warning,
			},
		},
	}, nil
}

func (h *NotificationHandler) DispatchNotification(ctx context.Context, input *DispatchNotificationInput) (*handlerutil.Out[notification.DispatchResponse], error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
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
		return nil, huma.Error500InternalServerError("dispatch failed")
	}

	return &handlerutil.Out[notification.DispatchResponse]{
		Body: base.ApiResponse[notification.DispatchResponse]{
			Success: true,
			Data:    dispatchResponse,
		},
	}, nil
}
