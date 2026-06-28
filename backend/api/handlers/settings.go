package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/category"
	"github.com/getarcaneapp/arcane/types/v2/search"
	"github.com/getarcaneapp/arcane/types/v2/settings"
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

// validateProjectsDirectoryValueInternal validates a projects directory value allowing:
// - Unix absolute paths (/...)
// - Windows drive paths (C:/..., C:\...)
// - Mapping format "container:host" where container is absolute Unix or Windows path
func validateProjectsDirectoryValueInternal(path string) error {
	switch {
	case projects.IsWindowsDrivePath(path):
		return nil
	case strings.Contains(path, ":"):
		parts := strings.SplitN(path, ":", 2)
		if len(parts) != 2 {
			return errors.New("projectsDirectory must be an absolute path or valid mapping format")
		}
		container := parts[0]
		if !strings.HasPrefix(container, "/") && !projects.IsWindowsDrivePath(container) {
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

// validateAbsoluteDirectoryPathInternal validates a plain absolute directory path allowing:
// - Unix absolute paths (/...)
// - Windows drive paths (C:/..., C:\...)
func validateAbsoluteDirectoryPathInternal(path string) error {
	switch {
	case projects.IsWindowsDrivePath(path):
		return nil
	case strings.HasPrefix(path, "/"):
		return nil
	default:
		return errors.New("must be an absolute path")
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
		Security:    []map[string][]string{},
	}, h.GetPublicSettings)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSettingsRead, h.GetSettings)

	humamw.RegisterWithPermission(api, huma.Operation{
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
	}, authz.PermSettingsWrite, h.UpdateSettings)

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

func filterSettingsCategoriesInternal(ps *authz.PermissionSet, categories []category.Category) []category.Category {
	if ps == nil {
		return []category.Category{}
	}
	filtered := make([]category.Category, 0, len(categories))
	for _, cat := range categories {
		if canAccessSettingsCategoryAtAnyScopeInternal(ps, cat.ID) {
			filtered = append(filtered, cat)
		}
	}
	return filtered
}

func canAccessSettingsCategoryAtAnyScopeInternal(ps *authz.PermissionSet, categoryID string) bool {
	if ps == nil {
		return false
	}
	if authz.CanAccessSettingsCategory(ps, categoryID, "") {
		return true
	}
	for envID := range ps.PerEnv {
		if authz.CanAccessSettingsCategory(ps, categoryID, envID) {
			return true
		}
	}
	return false
}

func (h *SettingsHandler) appendRuntimeSettingsInternal(settingsDto []settings.PublicSetting, includeAuthenticatedOnly bool) []settings.PublicSetting {
	if !includeAuthenticatedOnly {
		return settingsDto
	}

	uiConfigDisabled := false
	if h.cfg != nil {
		uiConfigDisabled = h.cfg.UIConfigurationDisabled
	}
	settingsDto = append(settingsDto, settings.PublicSetting{
		Key:   "uiConfigDisabled",
		Value: strconv.FormatBool(uiConfigDisabled),
		Type:  "boolean",
	})

	backupVolumeName := "arcane-backups"
	if h.cfg != nil && strings.TrimSpace(h.cfg.BackupVolumeName) != "" {
		backupVolumeName = h.cfg.BackupVolumeName
	}
	settingsDto = append(settingsDto, settings.PublicSetting{
		Key:   "backupVolumeName",
		Value: backupVolumeName,
		Type:  "string",
	})

	settingsDto = append(settingsDto, settings.PublicSetting{
		Key:   "edgeMTLSManagerCAAvailable",
		Value: strconv.FormatBool(hasGeneratedEdgeMTLSCAInternal(h.cfg)),
		Type:  "boolean",
	})

	if h.settingsService != nil {
		cfg := h.settingsService.GetSettingsConfig()
		depotConfigured := strings.TrimSpace(cfg.DepotProjectId.Value) != "" && strings.TrimSpace(cfg.DepotToken.Value) != ""
		settingsDto = append(settingsDto, settings.PublicSetting{
			Key:   "depotConfigured",
			Value: strconv.FormatBool(depotConfigured),
			Type:  "boolean",
		})
	}

	return settingsDto
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
		settingsDto, err := proxyRemoteJSONInternal[[]settings.PublicSetting](ctx, h.environmentService, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings/public", nil)
		if err != nil {
			return nil, err
		}
		return &GetPublicSettingsOutput{Body: *settingsDto}, nil
	}

	settingsList := h.settingsService.ListSettings(models.SettingVisibilityPublic)

	var settingsDto []settings.PublicSetting
	if err := mapper.MapStructList(settingsList, &settingsDto); err != nil {
		return nil, huma.Error500InternalServerError((&common.SettingsMappingError{Err: err}).Error())
	}

	return &GetPublicSettingsOutput{Body: h.appendRuntimeSettingsInternal(settingsDto, false)}, nil
}

// GetSettings returns all settings for an environment.
func (h *SettingsHandler) GetSettings(ctx context.Context, input *GetSettingsInput) (*GetSettingsOutput, error) {
	if h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	isAdmin := ps.IsGlobalAdmin()

	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		settingsDto, err := proxyRemoteJSONInternal[[]settings.PublicSetting](ctx, h.environmentService, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings", nil)
		if err != nil {
			return nil, err
		}
		return &GetSettingsOutput{Body: *settingsDto}, nil
	}

	visibility := models.SettingVisibilityNonAdmin
	if isAdmin {
		visibility = models.SettingVisibilityAll
	}
	settingsList := h.settingsService.ListSettings(visibility)

	var settingsDto []settings.PublicSetting
	if err := mapper.MapStructList(settingsList, &settingsDto); err != nil {
		return nil, huma.Error500InternalServerError((&common.SettingsMappingError{Err: err}).Error())
	}

	return &GetSettingsOutput{Body: h.appendRuntimeSettingsInternal(settingsDto, true)}, nil
}

// UpdateSettings updates settings for an environment.
func (h *SettingsHandler) UpdateSettings(ctx context.Context, input *UpdateSettingsInput) (*UpdateSettingsOutput, error) {
	if h.settingsService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.validateSettingsUpdateInput(input.Body); err != nil {
		return nil, err
	}

	if input.EnvironmentID != "0" {
		return h.updateSettingsForRemoteEnvironment(ctx, input)
	}

	return h.updateSettingsForLocalEnvironment(ctx, input.Body)
}

func (h *SettingsHandler) validateSettingsUpdateInput(input settings.Update) error {
	// Validate projects directory if provided and changed from current value.
	// Skip validation when the value matches the current (possibly env-overridden) setting
	// so that saving unrelated settings doesn't fail due to env-provided directory formats.
	if input.ProjectsDirectory != nil && *input.ProjectsDirectory != "" {
		currentDir := h.settingsService.GetSettingsConfig().ProjectsDirectory.Value
		if *input.ProjectsDirectory != currentDir {
			if err := validateProjectsDirectoryValueInternal(*input.ProjectsDirectory); err != nil {
				return huma.Error400BadRequest(err.Error())
			}
		}
	}

	if input.SwarmStackSourcesDirectory != nil && *input.SwarmStackSourcesDirectory != "" {
		currentDir := h.settingsService.GetSettingsConfig().SwarmStackSourcesDirectory.Value
		if *input.SwarmStackSourcesDirectory != currentDir {
			if err := validateAbsoluteDirectoryPathInternal(*input.SwarmStackSourcesDirectory); err != nil {
				return huma.Error400BadRequest("swarmStackSourcesDirectory " + err.Error())
			}
		}
	}

	return nil
}

func (h *SettingsHandler) updateSettingsForRemoteEnvironment(ctx context.Context, input *UpdateSettingsInput) (*UpdateSettingsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}

	// Check if trying to update auth settings on non-local environment.
	if hasAuthSettingsUpdateInternal(input.Body) {
		return nil, huma.Error403Forbidden((&common.AuthSettingsUpdateError{}).Error())
	}

	apiResp, err := proxyRemoteJSONInternal[base.ApiResponse[[]settings.SettingDto]](ctx, h.environmentService, input.EnvironmentID, http.MethodPut, "/api/environments/0/settings", input.Body)
	if err != nil {
		return nil, err
	}

	return &UpdateSettingsOutput{Body: *apiResp}, nil
}

func (h *SettingsHandler) updateSettingsForLocalEnvironment(ctx context.Context, input settings.Update) (*UpdateSettingsOutput, error) {
	if input.ProjectsDirectory != nil && *input.ProjectsDirectory != "" {
		currentDir := h.settingsService.GetSettingsConfig().ProjectsDirectory.Value
		if *input.ProjectsDirectory != currentDir {
			resolved, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(*input.ProjectsDirectory))
			if err != nil {
				return nil, huma.Error400BadRequest(fmt.Sprintf("cannot use projects directory %q: %v", *input.ProjectsDirectory, err))
			}
			f, err := os.Open(resolved)
			if err != nil {
				return nil, huma.Error400BadRequest(fmt.Sprintf("cannot read projects directory %q: %v", resolved, err))
			}
			if err := f.Close(); err != nil {
				return nil, huma.Error400BadRequest(fmt.Sprintf("cannot read projects directory %q: %v", resolved, err))
			}
		}
	}

	updatedSettings, err := h.settingsService.UpdateSettings(ctx, input)
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

func hasAuthSettingsUpdateInternal(req settings.Update) bool {
	return req.AuthLocalEnabled != nil || req.OidcEnabled != nil ||
		req.AuthSessionTimeout != nil || req.AuthPasswordPolicy != nil ||
		req.OidcClientId != nil ||
		req.OidcClientSecret != nil || req.OidcIssuerUrl != nil ||
		req.OidcScopes != nil ||
		req.OidcMergeAccounts != nil ||
		req.OidcSkipTlsVerify != nil || req.OidcAutoRedirectToProvider != nil ||
		req.OidcProviderName != nil || req.OidcProviderLogoUrl != nil ||
		req.OidcGroupsClaim != nil
}

// Search searches settings by query.
func (h *SettingsHandler) Search(ctx context.Context, input *SearchSettingsInput) (*SearchSettingsOutput, error) {
	if h.settingsSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest((&common.QueryParameterRequiredError{}).Error())
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	results := h.settingsSearchService.Search(input.Body.Query)
	results.Results = filterSettingsCategoriesInternal(ps, results.Results)
	results.Count = len(results.Results)
	return &SearchSettingsOutput{Body: results}, nil
}

// GetCategories returns all available settings categories.
func (h *SettingsHandler) GetCategories(ctx context.Context, input *struct{}) (*GetCategoriesOutput, error) {
	if h.settingsSearchService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	ps, _ := humamw.PermissionsFromContext(ctx)
	categories := filterSettingsCategoriesInternal(ps, h.settingsSearchService.GetSettingsCategories())
	return &GetCategoriesOutput{Body: categories}, nil
}
