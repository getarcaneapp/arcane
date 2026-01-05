package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/notification"
)

type NotificationHandler struct {
	notificationService *services.NotificationService
	appriseService      *services.AppriseService
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

type DeleteNotificationSettingsOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type TestNotificationInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Provider      string `path:"provider" doc:"Provider"`
	Type          string `query:"type" default:"simple"`
}

type TestNotificationOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type GetAppriseSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetAppriseSettingsOutput struct {
	Body notification.AppriseResponse
}

type CreateOrUpdateAppriseSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          notification.AppriseUpdate
}

type CreateOrUpdateAppriseSettingsOutput struct {
	Body notification.AppriseResponse
}

type TestAppriseNotificationInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type TestAppriseNotificationOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// RegisterNotifications registers notification endpoints.
func RegisterNotifications(api huma.API, notificationSvc *services.NotificationService, appriseSvc *services.AppriseService) {
	h := &NotificationHandler{
		notificationService: notificationSvc,
		appriseService:      appriseSvc,
	}

	huma.Register(api, huma.Operation{
		OperationID: "get-all-notification-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/settings",
		Summary:     "Get all notification settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetAllNotificationSettings)

	huma.Register(api, huma.Operation{
		OperationID: "get-notification-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Get notification settings by provider",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetNotificationSettings)

	huma.Register(api, huma.Operation{
		OperationID: "create-or-update-notification-settings",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/settings",
		Summary:     "Create or update notification settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateOrUpdateNotificationSettings)

	huma.Register(api, huma.Operation{
		OperationID: "delete-notification-settings",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/notifications/settings/{provider}",
		Summary:     "Delete notification settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeleteNotificationSettings)

	huma.Register(api, huma.Operation{
		OperationID: "test-notification",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/test/{provider}",
		Summary:     "Test notification",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.TestNotification)

	huma.Register(api, huma.Operation{
		OperationID: "get-apprise-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/notifications/apprise",
		Summary:     "Get Apprise settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetAppriseSettings)

	huma.Register(api, huma.Operation{
		OperationID: "create-or-update-apprise-settings",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/apprise",
		Summary:     "Create or update Apprise settings",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateOrUpdateAppriseSettings)

	huma.Register(api, huma.Operation{
		OperationID: "test-apprise-notification",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/notifications/apprise/test",
		Summary:     "Test Apprise notification",
		Tags:        []string{"Notifications"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.TestAppriseNotification)
}

func (h *NotificationHandler) GetAllNotificationSettings(ctx context.Context, input *GetAllNotificationSettingsInput) (*GetAllNotificationSettingsOutput, error) {
	if err := checkAdmin(ctx); err != nil {
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
			Config:   base.JsonObject(setting.Config),
		}
	}

	return &GetAllNotificationSettingsOutput{Body: responses}, nil
}

func (h *NotificationHandler) GetNotificationSettings(ctx context.Context, input *GetNotificationSettingsInput) (*GetNotificationSettingsOutput, error) {
	if err := checkAdmin(ctx); err != nil {
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
		Config:   base.JsonObject(settings.Config),
	}

	return &GetNotificationSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) CreateOrUpdateNotificationSettings(ctx context.Context, input *CreateOrUpdateNotificationSettingsInput) (*CreateOrUpdateNotificationSettingsOutput, error) {
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Body.Provider)
	if provider != models.NotificationProviderDiscord && provider != models.NotificationProviderEmail {
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
		Config:   base.JsonObject(settings.Config),
	}

	return &CreateOrUpdateNotificationSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) DeleteNotificationSettings(ctx context.Context, input *DeleteNotificationSettingsInput) (*DeleteNotificationSettingsOutput, error) {
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Provider)

	if err := h.notificationService.DeleteSettings(ctx, provider); err != nil {
		return nil, huma.Error500InternalServerError((&common.NotificationSettingsDeletionError{Err: err}).Error())
	}

	return &DeleteNotificationSettingsOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Settings deleted successfully"},
		},
	}, nil
}

func (h *NotificationHandler) TestNotification(ctx context.Context, input *TestNotificationInput) (*TestNotificationOutput, error) {
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}
	provider := models.NotificationProvider(input.Provider)

	if err := h.notificationService.TestNotification(ctx, provider, input.Type); err != nil {
		return nil, huma.Error500InternalServerError((&common.NotificationTestError{Err: err}).Error())
	}

	return &TestNotificationOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Test notification sent successfully"},
		},
	}, nil
}

func (h *NotificationHandler) GetAppriseSettings(ctx context.Context, input *GetAppriseSettingsInput) (*GetAppriseSettingsOutput, error) {
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}
	settings, err := h.appriseService.GetSettings(ctx)
	if err != nil {
		return nil, huma.Error404NotFound((&common.AppriseSettingsNotFoundError{}).Error())
	}

	response := notification.AppriseResponse{
		ID:                 settings.ID,
		APIURL:             settings.APIURL,
		Enabled:            settings.Enabled,
		ImageUpdateTag:     settings.ImageUpdateTag,
		ContainerUpdateTag: settings.ContainerUpdateTag,
	}

	return &GetAppriseSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) CreateOrUpdateAppriseSettings(ctx context.Context, input *CreateOrUpdateAppriseSettingsInput) (*CreateOrUpdateAppriseSettingsOutput, error) {
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}
	if input.Body.Enabled && input.Body.APIURL == "" {
		return nil, huma.Error400BadRequest("API URL is required when Apprise is enabled")
	}

	settings, err := h.appriseService.CreateOrUpdateSettings(
		ctx,
		input.Body.APIURL,
		input.Body.Enabled,
		input.Body.ImageUpdateTag,
		input.Body.ContainerUpdateTag,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.AppriseSettingsUpdateError{Err: err}).Error())
	}

	response := notification.AppriseResponse{
		ID:                 settings.ID,
		APIURL:             settings.APIURL,
		Enabled:            settings.Enabled,
		ImageUpdateTag:     settings.ImageUpdateTag,
		ContainerUpdateTag: settings.ContainerUpdateTag,
	}

	return &CreateOrUpdateAppriseSettingsOutput{Body: response}, nil
}

func (h *NotificationHandler) TestAppriseNotification(ctx context.Context, input *TestAppriseNotificationInput) (*TestAppriseNotificationOutput, error) {
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}
	if err := h.appriseService.TestNotification(ctx); err != nil {
		return nil, huma.Error500InternalServerError((&common.AppriseTestError{Err: err}).Error())
	}

	return &TestAppriseNotificationOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Test notification sent successfully"},
		},
	}, nil
}
