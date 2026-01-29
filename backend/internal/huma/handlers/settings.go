package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pathmapper"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/category"
	"github.com/getarcaneapp/arcane/types/search"
	"github.com/getarcaneapp/arcane/types/settings"
)

// SettingsHandler provides Huma-based settings management endpoints.
type SettingsHandler struct {
	settingsService       *services.SettingsService
	settingsSearchService *services.SettingsSearchService
	environmentService    *services.EnvironmentService
	cfg                   *config.Config
}

// --- Huma Input/Output Wrappers ---

type GetSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSettingsOutput struct {
	Body []settings.PublicSetting
}

type GetPublicSettingsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetPublicSettingsOutput struct {
	Body []settings.PublicSetting
}

type UpdateSettingsInput struct {
	EnvironmentID string          `path:"id" doc:"Environment ID"`
	Body          settings.Update `doc:"Settings update data"`
}

type UpdateSettingsOutput struct {
	Body base.ApiResponse[[]settings.SettingDto]
}

type SearchSettingsInput struct {
	Body search.Request `doc:"Search query"`
}

type SearchSettingsOutput struct {
	Body search.Response
}

type GetCategoriesOutput struct {
	Body []category.Category
}

// validateProjectsDirectoryValue validates a projects directory value allowing:
// - Unix absolute paths (/...)
// - Windows drive paths (C:/..., C:\...)
// - Mapping format "container:host" where container is absolute Unix or Windows path
func validateProjectsDirectoryValue(path string) error {
	switch {
	case pathmapper.IsWindowsDrivePath(path):
		return nil
	case strings.Contains(path, ":"):
		parts := strings.SplitN(path, ":", 2)
		if len(parts) != 2 {
			return errors.New("projectsDirectory must be an absolute path or valid mapping format")
		}
		container := parts[0]
		if !strings.HasPrefix(container, "/") && !pathmapper.IsWindowsDrivePath(container) {
			return errors.New("projectsDirectory mapping format: container path must be absolute")
		}
		return nil
	default:
		if !strings.HasPrefix(path, "/") {
			return errors.New("projectsDirectory must be an absolute path starting with '/'")
		}
		return nil
	}
}

// RegisterSettings registers settings management routes using Huma.
func RegisterSettings(api huma.API, settingsService *services.SettingsService, settingsSearchService *services.SettingsSearchService, environmentService *services.EnvironmentService, cfg *config.Config) {
	h := &SettingsHandler{
		settingsService:       settingsService,
		settingsSearchService: settingsSearchService,
		environmentService:    environmentService,
		cfg:                   cfg,
	}

	// Environment-scoped settings endpoints
	huma.Register(api, huma.Operation{
		OperationID: "get-public-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/settings/public",
		Summary:     "Get public settings",
		Description: "Get all public settings for an environment",
		Tags:        []string{"Settings"},
	}, h.GetPublicSettings)

	huma.Register(api, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/settings",
		Summary:     "Get settings",
		Description: "Get all settings for an environment",
		Tags:        []string{"Settings"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetSettings)

	huma.Register(api, huma.Operation{
		OperationID: "update-settings",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/settings",
		Summary:     "Update settings",
		Description: "Update settings for an environment",
		Tags:        []string{"Settings"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateSettings)

	// Top-level settings endpoints (not environment-scoped)
	huma.Register(api, huma.Operation{
		OperationID: "search-settings",
		Method:      http.MethodPost,
		Path:        "/settings/search",
		Summary:     "Search settings",
		Description: "Search settings categories and individual settings by query",
		Tags:        []string{"Settings"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.Search)

	huma.Register(api, huma.Operation{
		OperationID: "get-settings-categories",
		Method:      http.MethodGet,
		Path:        "/settings/categories",
		Summary:     "Get settings categories",
		Description: "Get all available settings categories with metadata",
		Tags:        []string{"Settings"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetCategories)
}

// GetPublicSettings returns public settings for an environment.
func (h *SettingsHandler) GetPublicSettings(ctx context.Context, input *GetPublicSettingsInput) (*GetPublicSettingsOutput, error) {
	if h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		respBody, statusCode, err := h.environmentService.ProxyRequest(ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings/public", nil)
		if err != nil {
			return nil, huma.Error502BadGateway("failed to proxy request to environment: " + err.Error())
		}
		if statusCode != http.StatusOK {
			return nil, huma.NewError(statusCode, "environment returned error: "+string(respBody), nil)
		}
		var settingsDto []settings.PublicSetting
		if err := json.Unmarshal(respBody, &settingsDto); err != nil {
			return nil, huma.Error500InternalServerError("failed to decode environment response: " + err.Error())
		}
		return &GetPublicSettingsOutput{Body: settingsDto}, nil
	}

	settingsList := h.settingsService.ListSettings(false)

	var settingsDto []settings.PublicSetting
	if err := mapper.MapStructList(settingsList, &settingsDto); err != nil {
		return nil, huma.Error500InternalServerError((&common.SettingsMappingError{Err: err}).Error())
	}

	// Add UI config disabled setting
	uiConfigDisabled := false
	if h.cfg != nil {
		uiConfigDisabled = h.cfg.UIConfigurationDisabled
	}
	settingsDto = append(settingsDto, settings.PublicSetting{
		Key:   "uiConfigDisabled",
		Value: strconv.FormatBool(uiConfigDisabled),
		Type:  "boolean",
	})

	return &GetPublicSettingsOutput{Body: settingsDto}, nil
}

// GetSettings returns all settings for an environment.
func (h *SettingsHandler) GetSettings(ctx context.Context, input *GetSettingsInput) (*GetSettingsOutput, error) {
	if h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	isAdmin := humamw.IsAdminFromContext(ctx)

	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		respBody, statusCode, err := h.environmentService.ProxyRequest(ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings", nil)
		if err != nil {
			return nil, huma.Error502BadGateway("failed to proxy request to environment: " + err.Error())
		}
		if statusCode != http.StatusOK {
			return nil, huma.NewError(statusCode, "environment returned error: "+string(respBody), nil)
		}
		var settingsDto []settings.PublicSetting
		if err := json.Unmarshal(respBody, &settingsDto); err != nil {
			return nil, huma.Error500InternalServerError("failed to decode environment response: " + err.Error())
		}
		return &GetSettingsOutput{Body: settingsDto}, nil
	}

	showAll := isAdmin
	settingsList := h.settingsService.ListSettings(showAll)

	var settingsDto []settings.PublicSetting
	if err := mapper.MapStructList(settingsList, &settingsDto); err != nil {
		return nil, huma.Error500InternalServerError((&common.SettingsMappingError{Err: err}).Error())
	}

	// Add UI config disabled setting
	uiConfigDisabled := false
	if h.cfg != nil {
		uiConfigDisabled = h.cfg.UIConfigurationDisabled
	}
	settingsDto = append(settingsDto, settings.PublicSetting{
		Key:   "uiConfigDisabled",
		Value: strconv.FormatBool(uiConfigDisabled),
		Type:  "boolean",
	})

	return &GetSettingsOutput{Body: settingsDto}, nil
}

// UpdateSettings updates settings for an environment.
func (h *SettingsHandler) UpdateSettings(ctx context.Context, input *UpdateSettingsInput) (*UpdateSettingsOutput, error) {
	if h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	// Validate projects directory if provided
	if input.Body.ProjectsDirectory != nil && *input.Body.ProjectsDirectory != "" {
		if err := validateProjectsDirectoryValue(*input.Body.ProjectsDirectory); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
	}

	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}

		// Check if trying to update auth settings on non-local environment
		req := input.Body
		if req.AuthLocalEnabled != nil || req.OidcEnabled != nil ||
			req.AuthSessionTimeout != nil || req.AuthPasswordPolicy != nil ||
			req.AuthOidcConfig != nil || req.OidcClientId != nil ||
			req.OidcClientSecret != nil || req.OidcIssuerUrl != nil ||
			req.OidcScopes != nil || req.OidcAdminClaim != nil ||
			req.OidcAdminValue != nil || req.OidcMergeAccounts != nil ||
			req.OidcSkipTlsVerify != nil || req.OidcAutoRedirectToProvider != nil ||
			req.OidcProviderName != nil || req.OidcProviderLogoUrl != nil {
			return nil, huma.Error403Forbidden((&common.AuthSettingsUpdateError{}).Error())
		}

		body, err := json.Marshal(input.Body)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to marshal request body: " + err.Error())
		}

		respBody, statusCode, err := h.environmentService.ProxyRequest(ctx, input.EnvironmentID, http.MethodPut, "/api/environments/0/settings", body)
		if err != nil {
			return nil, huma.Error502BadGateway("failed to proxy request to environment: " + err.Error())
		}
		if statusCode != http.StatusOK {
			return nil, huma.NewError(statusCode, "environment returned error: "+string(respBody), nil)
		}

		var apiResp base.ApiResponse[[]settings.SettingDto]
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			return nil, huma.Error500InternalServerError("failed to decode environment response: " + err.Error())
		}

		return &UpdateSettingsOutput{Body: apiResp}, nil
	}

	updatedSettings, err := h.settingsService.UpdateSettings(ctx, input.Body)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.SettingsUpdateError{Err: err}).Error())
	}

	settingDtos := make([]settings.SettingDto, 0, len(updatedSettings))
	for _, setting := range updatedSettings {
		settingDtos = append(settingDtos, settings.SettingDto{
			PublicSetting: settings.PublicSetting{
				Key:   setting.Key,
				Type:  "string",
				Value: setting.Value,
			},
		})
	}

	return &UpdateSettingsOutput{
		Body: base.ApiResponse[[]settings.SettingDto]{
			Success: true,
			Data:    settingDtos,
		},
	}, nil
}

// Search searches settings by query.
func (h *SettingsHandler) Search(ctx context.Context, input *SearchSettingsInput) (*SearchSettingsOutput, error) {
	if h.settingsSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest((&common.QueryParameterRequiredError{}).Error())
	}

	results := h.settingsSearchService.Search(input.Body.Query)
	return &SearchSettingsOutput{Body: results}, nil
}

// GetCategories returns all available settings categories.
func (h *SettingsHandler) GetCategories(ctx context.Context, input *struct{}) (*GetCategoriesOutput, error) {
	if h.settingsSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	categories := h.settingsSearchService.GetSettingsCategories()
	return &GetCategoriesOutput{Body: categories}, nil
}
