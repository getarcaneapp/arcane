package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/notification"
)

type notificationHandler struct {
	notificationService *services.NotificationService
	config              *config.Config
}

type getAllNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type getAllNotificationSettingsOutput struct {
	Body []notification.Response
}

type getNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
}

type getNotificationSettingsOutput struct {
	Body notification.Response
}

type createOrUpdateNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          notification.Update
}

type createOrUpdateNotificationSettingsOutput struct {
	Body notification.Response
}

type deleteNotificationSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
}

type deleteNotificationSettingsOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type testNotificationInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
	Type          string `query:"type" default:"simple"`
}

type testNotificationOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type dispatchNotificationInput struct {
	APIKey string `header:"X-API-Key" doc:"Remote environment access token"`
	Body   notification.DispatchRequest
}

type dispatchNotificationOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

var supportedNotificationTestTypes = map[string]struct{}{
	"simple":              {},
	"image-update":        {},
	"batch-image-update":  {},
	"vulnerability-found": {},
	"prune-report":        {},
	"auto-heal":           {},
}

func normalizeNotificationTestType(testType string) string {
	normalized := strings.TrimSpace(testType)
	if normalized == "" {
		return "simple"
	}
	return normalized
}

func isSupportedNotificationTestType(testType string) bool {
	_, ok := supportedNotificationTestTypes[testType]
	return ok
}

// RegisterNotifications registers notification endpoints.
func RegisterNotifications(api huma.API, notificationSvc *services.NotificationService, cfg *config.Config) {
	h := &notificationHandler{
		notificationService: notificationSvc,
		config:              cfg,
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-all-notification-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/settings",
		Summary:     "Get all notification settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNotificationsManage, h.getAllNotificationSettingsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-notification-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Get notification settings by provider",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNotificationsManage, h.getNotificationSettingsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-or-update-notification-settings",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/settings",
		Summary:     "Create or update notification settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNotificationsManage, h.createOrUpdateNotificationSettingsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "delete-notification-settings",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Delete notification settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNotificationsManage, h.deleteNotificationSettingsInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "test-notification",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/test/{provider}",
		Summary:     "Test notification",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermNotificationsManage, h.testNotificationInternal)

	huma.Register(api, huma.Operation{
		OperationID: "dispatch-notification",
		Method:      http.MethodPost,
		Path:        "/notifications/dispatch",
		Summary:     "Dispatch notification from remote agent to manager",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"ApiKeyAuth": {}}},
		Middlewares: humamw.RequirePermission(api, authz.PermNotificationsManage),
	}, h.dispatchNotificationInternal)
}

func (h *notificationHandler) rejectIfAgentModeInternal() error {
	if h.config != nil && h.config.AgentMode {
		return huma.Error400BadRequest("notifications are managed on the Arcane manager")
	}
	return nil
}

func (h *notificationHandler) getAllNotificationSettingsInternal(ctx context.Context, _ *getAllNotificationSettingsInput) (*getAllNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	settings, err := h.notificationService.GetAllSettings(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NotificationSettingsListError{Err: err}).Error())
	}

	responses := make([]notification.Response, len(settings))
	for i, setting := range settings {
		responses[i] = notification.Response{
			ID:       setting.ID,
			Provider: notification.Provider(setting.Provider),
			Enabled:  setting.Enabled,
			Config:   base.JsonObject(services.RedactNotificationConfigCredentials(setting.Provider, setting.Config)),
		}
	}

	return &getAllNotificationSettingsOutput{Body: responses}, nil
}

func (h *notificationHandler) getNotificationSettingsInternal(ctx context.Context, input *getNotificationSettingsInput) (*getNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Provider)

	settings, err := h.notificationService.GetSettingsByProvider(ctx, provider)
	if err != nil {
		return nil, huma.Error404NotFound((&common.NotificationSettingsNotFoundError{}).Error())
	}

	response := notification.Response{
		ID:       settings.ID,
		Provider: notification.Provider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   base.JsonObject(services.RedactNotificationConfigCredentials(settings.Provider, settings.Config)),
	}

	return &getNotificationSettingsOutput{Body: response}, nil
}

func (h *notificationHandler) createOrUpdateNotificationSettingsInternal(ctx context.Context, input *createOrUpdateNotificationSettingsInput) (*createOrUpdateNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Body.Provider)
	if !models.IsValidNotificationProvider(provider) {
		return nil, huma.Error400BadRequest((&common.InvalidNotificationProviderError{}).Error())
	}

	settings, err := h.notificationService.CreateOrUpdateSettings(
		ctx,
		provider,
		input.Body.Enabled,
		models.JSON(input.Body.Config),
	)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.NotificationSettingsUpdateError{Err: err}).Error())
	}

	response := notification.Response{
		ID:       settings.ID,
		Provider: notification.Provider(settings.Provider),
		Enabled:  settings.Enabled,
		Config:   base.JsonObject(services.RedactNotificationConfigCredentials(settings.Provider, settings.Config)),
	}

	return &createOrUpdateNotificationSettingsOutput{Body: response}, nil
}

func (h *notificationHandler) deleteNotificationSettingsInternal(ctx context.Context, input *deleteNotificationSettingsInput) (*deleteNotificationSettingsOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Provider)

	if err := h.notificationService.DeleteSettings(ctx, provider); err != nil {
		return nil, huma.Error500InternalServerError((&common.NotificationSettingsDeletionError{Err: err}).Error())
	}

	return &deleteNotificationSettingsOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Settings deleted successfully"},
		},
	}, nil
}

func (h *notificationHandler) testNotificationInternal(ctx context.Context, input *testNotificationInput) (*testNotificationOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Provider)
	testType := normalizeNotificationTestType(input.Type)
	if !isSupportedNotificationTestType(testType) {
		return nil, huma.Error400BadRequest("invalid notification test type")
	}

	if err := h.notificationService.TestNotification(ctx, input.EnvironmentID, provider, testType); err != nil {
		return nil, huma.Error500InternalServerError((&common.NotificationTestError{Err: err}).Error())
	}

	return &testNotificationOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Test notification sent successfully"},
		},
	}, nil
}

func (h *notificationHandler) dispatchNotificationInternal(ctx context.Context, input *dispatchNotificationInput) (*dispatchNotificationOutput, error) {
	if err := h.rejectIfAgentModeInternal(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		return nil, huma.Error401Unauthorized("missing remote environment access token")
	}
	if err := h.notificationService.DispatchNotification(ctx, input.APIKey, input.Body); err != nil {
		if errors.Is(err, services.ErrUnsupportedDispatchKind) {
			return nil, huma.Error400BadRequest("unsupported dispatch kind")
		}
		if errors.Is(err, services.ErrUnauthorizedNotificationDispatch) {
			return nil, huma.Error401Unauthorized("unauthorized")
		}
		return nil, huma.Error500InternalServerError("dispatch failed")
	}

	return &dispatchNotificationOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Notification dispatched successfully"},
		},
	}, nil
}
