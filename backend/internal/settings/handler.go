package settings

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/category"
	searchtypes "github.com/getarcaneapp/arcane/types/v2/search"
	"github.com/getarcaneapp/arcane/types/v2/settings"
	"go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.yaml.in/yaml/v4"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/telemetry"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

const (
	projectWorkspaceMaxFileSizeSettingKey = "projectWorkspaceMaxFileSizeMb"
	volumeWorkspaceMaxFileSizeSettingKey  = "volumeWorkspaceMaxFileSizeMb"
)

// SettingsHandler provides Huma-based settings management endpoints.
type SettingsHandler struct {
	settingsService       *SettingsService
	settingsSearchService *SettingsSearchService
	proxyRemoteJSON       handlerutil.RemoteJSONProxy
	cfg                   *config.Config
}

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

type SearchSettingsInput struct {
	Body searchtypes.Request `doc:"Search query"`
}

type SearchSettingsOutput struct {
	Body searchtypes.Response
}

type GetCategoriesOutput struct {
	Body []category.Category
}

func (h *SettingsHandler) appendRuntimeSettings(settingsDto []settings.PublicSetting, includeAuthenticatedOnly bool) []settings.PublicSetting {
	relay := h.cfg != nil && !h.cfg.AgentMode
	settingsDto = append(settingsDto,
		settings.PublicSetting{Key: "frontendTracingEnabled", Value: strconv.FormatBool(relay && telemetry.Endpoint("traces") != ""), Type: "boolean"},
		settings.PublicSetting{Key: "frontendMetricsEnabled", Value: strconv.FormatBool(relay && telemetry.Endpoint("metrics") != ""), Type: "boolean"},
		settings.PublicSetting{Key: "frontendLogsEnabled", Value: strconv.FormatBool(relay && telemetry.Endpoint("logs") != ""), Type: "boolean"},
	)
	if !includeAuthenticatedOnly {
		return settingsDto
	}

	cfg := cmp.Or(h.cfg, &config.Config{})
	var edgeCfg *edge.Config
	if h.cfg != nil {
		edgeCfg = &edge.Config{
			EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
			EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
			EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
		}
	}
	_, edgeMTLSCAErr := edge.AvailableManagerMTLSCAPath(edgeCfg)
	projectWorkspaceMaxFileSizeMB := workspace.EffectiveMaxFileSizeMB(cfg.ProjectWorkspaceMaxFileSizeMB)
	volumeWorkspaceMaxFileSizeMB := workspace.EffectiveMaxFileSizeMB(cfg.VolumeWorkspaceMaxFileSizeMB)
	settingsDto = append(settingsDto,
		settings.PublicSetting{Key: "uiConfigDisabled", Value: strconv.FormatBool(cfg.UIConfigurationDisabled), Type: "boolean"},
		settings.PublicSetting{Key: projectWorkspaceMaxFileSizeSettingKey, Value: strconv.Itoa(projectWorkspaceMaxFileSizeMB), Type: "number"},
		settings.PublicSetting{Key: volumeWorkspaceMaxFileSizeSettingKey, Value: strconv.Itoa(volumeWorkspaceMaxFileSizeMB), Type: "number"},
		settings.PublicSetting{
			Key:   "backupVolumeName",
			Value: kit.Ternary(strings.TrimSpace(cfg.BackupVolumeName) != "", cfg.BackupVolumeName, "arcane-backups"),
			Type:  "string",
		},
		settings.PublicSetting{Key: "edgeMTLSManagerCAAvailable", Value: strconv.FormatBool(edgeMTLSCAErr == nil), Type: "boolean"},
	)

	if h.settingsService != nil {
		settingsCfg := h.settingsService.GetSettingsConfig()
		depotConfigured := strings.TrimSpace(settingsCfg.DepotProjectId.Value) != "" && strings.TrimSpace(settingsCfg.DepotToken.Value) != ""
		settingsDto = append(settingsDto,
			settings.PublicSetting{Key: "depotConfigured", Value: strconv.FormatBool(depotConfigured), Type: "boolean"},
			settings.PublicSetting{Key: "enableGravatarEnvForced", Value: strconv.FormatBool(h.settingsService.IsEnvOverrideActive("enableGravatar")), Type: "boolean"},
		)
	}

	return settingsDto
}

// GetPublicSettings returns public settings for an environment.
func (h *SettingsHandler) GetPublicSettings(ctx context.Context, input *GetPublicSettingsInput) (*GetPublicSettingsOutput, error) {
	if input.EnvironmentID != "0" {
		settingsDto, err := h.proxyRemoteJSON.JSON[[]settings.PublicSetting](ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings/public", nil)
		if err != nil {
			return nil, err
		}
		return &GetPublicSettingsOutput{Body: *settingsDto}, nil
	}

	settingsList := h.settingsService.ListSettings(SettingVisibilityPublic)

	settingsDto, err := mapping.MapSlice[SettingVariable, settings.PublicSetting](settingsList)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map settings")
	}

	return &GetPublicSettingsOutput{Body: h.appendRuntimeSettings(settingsDto, false)}, nil
}

// GetSettings returns all settings for an environment.
func (h *SettingsHandler) GetSettings(ctx context.Context, input *GetSettingsInput) (*GetSettingsOutput, error) {
	ps, _ := middleware.PermissionsFromContext(ctx)
	isAdmin := ps.IsGlobalAdmin()
	visibility := kit.Ternary(isAdmin, SettingVisibilityAll, SettingVisibilityNonAdmin)

	if input.EnvironmentID != "0" {
		settingsDto, err := h.proxyRemoteJSON.JSON[[]settings.PublicSetting](ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings", nil)
		if err != nil {
			return nil, err
		}
		if !isAdmin {
			allowedKeys := make(map[string]struct{})
			for _, setting := range h.settingsService.ListSettings(visibility) {
				allowedKeys[setting.Key] = struct{}{}
			}
			allowedKeys[projectWorkspaceMaxFileSizeSettingKey] = struct{}{}
			allowedKeys[volumeWorkspaceMaxFileSizeSettingKey] = struct{}{}
			*settingsDto = slices.DeleteFunc(*settingsDto, func(setting settings.PublicSetting) bool {
				_, ok := allowedKeys[setting.Key]
				return !ok
			})
		}
		return &GetSettingsOutput{Body: *settingsDto}, nil
	}

	settingsList := h.settingsService.ListSettings(visibility)

	settingsDto, err := mapping.MapSlice[SettingVariable, settings.PublicSetting](settingsList)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to map settings")
	}

	return &GetSettingsOutput{Body: h.appendRuntimeSettings(settingsDto, true)}, nil
}

// UpdateSettings updates settings for an environment.
func (h *SettingsHandler) UpdateSettings(ctx context.Context, input *UpdateSettingsInput) (*handlerutil.Out[[]settings.SettingDto], error) {
	update := input.Body
	// Only validate changed directories so env-provided values do not block unrelated saves.
	currentCfg := h.settingsService.GetSettingsConfig()
	projectsDir := ""
	if update.ProjectsDirectory != nil && *update.ProjectsDirectory != currentCfg.ProjectsDirectory.Value {
		projectsDir = *update.ProjectsDirectory
	}
	if projectsDir != "" && !projects.IsWindowsDrivePath(projectsDir) {
		// Mapping format is "container:host"; the container path must be absolute.
		if container, _, isMapping := strings.Cut(projectsDir, ":"); !strings.HasPrefix(container, "/") {
			return nil, huma.Error400BadRequest(kit.Ternary(isMapping,
				"projectsDirectory mapping format: container path must be absolute",
				"projectsDirectory must be an absolute path starting with '/'"))
		}
	}

	if dir := update.SwarmStackSourcesDirectory; dir != nil && *dir != "" && *dir != currentCfg.SwarmStackSourcesDirectory.Value &&
		!projects.IsWindowsDrivePath(*dir) && !strings.HasPrefix(*dir, "/") {
		return nil, huma.Error400BadRequest("swarmStackSourcesDirectory must be an absolute path")
	}

	if update.AvatarMaxUploadSizeMb != nil && strings.TrimSpace(*update.AvatarMaxUploadSizeMb) != "" {
		value, err := strconv.Atoi(strings.TrimSpace(*update.AvatarMaxUploadSizeMb))
		if err != nil || value < 1 || value > 50 {
			return nil, huma.Error400BadRequest("avatarMaxUploadSizeMb must be between 1 and 50")
		}
	}

	if update.TrivyConfig != nil && strings.TrimSpace(*update.TrivyConfig) != "" {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(*update.TrivyConfig), &doc); err != nil {
			return nil, huma.Error400BadRequest("trivyConfig must be a YAML mapping: " + err.Error())
		}
		if doc == nil {
			return nil, huma.Error400BadRequest("trivyConfig must be a YAML mapping")
		}
	}

	if input.EnvironmentID != "0" {
		if update.AuthLocalEnabled != nil || update.OidcEnabled != nil || update.AuthSessionTimeout != nil || update.AuthPasswordPolicy != nil ||
			update.OidcClientId != nil || update.OidcClientSecret != nil || update.OidcIssuerUrl != nil || update.OidcScopes != nil ||
			update.OidcMergeAccounts != nil || update.OidcSkipTlsVerify != nil || update.OidcAutoRedirectToProvider != nil ||
			update.OidcProviderName != nil || update.OidcProviderLogoUrl != nil || update.OidcGroupsClaim != nil {
			return nil, huma.Error403Forbidden("Authentication settings can only be updated from the main environment")
		}
		apiResp, err := h.proxyRemoteJSON.JSON[base.ApiResponse[[]settings.SettingDto]](ctx, input.EnvironmentID, http.MethodPut, "/api/environments/0/settings", update)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[[]settings.SettingDto]{Body: *apiResp}, nil
	}

	if projectsDir != "" {
		resolved, err := projects.GetProjectsDirectory(ctx, strings.TrimSpace(projectsDir))
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("cannot use projects directory %q: %v", projectsDir, err))
		}
		// os rather than acfs: the directory is validated before it becomes a confinement root.
		f, err := os.Open(resolved)
		if err == nil {
			err = f.Close()
		}
		if err != nil {
			return nil, huma.Error400BadRequest(fmt.Sprintf("cannot read projects directory %q: %v", resolved, err))
		}
	}

	updatedSettings, err := h.settingsService.UpdateSettings(ctx, update)
	if err != nil {
		apiErr := common.ToAPIError(err)
		if apiErr.HTTPStatus() == http.StatusInternalServerError {
			return nil, huma.Error500InternalServerError("Failed to update settings")
		}
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	settingDtos := make([]settings.SettingDto, 0, len(updatedSettings))
	for _, setting := range updatedSettings {
		settingDtos = append(settingDtos, settings.SettingDto{Key: setting.Key, Type: "string", Value: setting.Value})
	}

	return &handlerutil.Out[[]settings.SettingDto]{
		Body: base.ApiResponse[[]settings.SettingDto]{Success: true, Data: settingDtos},
	}, nil
}

// Search searches settings by query.
func (h *SettingsHandler) Search(ctx context.Context, input *SearchSettingsInput) (*SearchSettingsOutput, error) {
	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest("Query parameter is required")
	}

	categories, err := h.GetCategories(ctx, nil)
	if err != nil {
		return nil, err
	}
	results := search.Search(categories.Body, input.Body.Query, searchtypes.SettingsProfile)
	if results.Results == nil {
		results.Results = []category.Category{}
	}
	return &SearchSettingsOutput{Body: results}, nil
}

// GetCategories returns the settings categories reachable at any scope.
func (h *SettingsHandler) GetCategories(ctx context.Context, input *struct{}) (*GetCategoriesOutput, error) {
	categories := []category.Category{}
	ps, _ := middleware.PermissionsFromContext(ctx)
	if ps == nil {
		return &GetCategoriesOutput{Body: categories}, nil
	}
	scopes := append([]string{""}, slices.Collect(maps.Keys(ps.PerEnv))...)
	for _, cat := range h.settingsSearchService.GetSettingsCategories() {
		if slices.ContainsFunc(scopes, func(envID string) bool { return authz.CanAccessSettingsCategory(ps, cat.ID, envID) }) {
			categories = append(categories, cat)
		}
	}
	return &GetCategoriesOutput{Body: categories}, nil
}
