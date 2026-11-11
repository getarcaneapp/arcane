package settings

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/search"
	"getarcane.dev/arcane/v3/internal/telemetry"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	SettingsHandler struct {
		settingsService       *SettingsService
		settingsSearchService *SettingsSearchService
		proxyRemoteJSON       handlerutil.RemoteJSONProxy
		cfg                   *config.Config
		mtls                  *agent.MTLS
	}

	SettingsSnapshotOutput struct {
		Body types.SettingsSnapshot
	}

	UpdateSettingsInput struct {
		Body          types.SettingsUpdate `doc:"Setting keys mapped to their new values"`
		EnvironmentID string               `path:"id" doc:"Environment ID"`
	}

	UpdateScopedSettingsInput struct {
		Body types.SettingsUpdate `doc:"Setting keys mapped to their new values"`
	}

	SearchSettingsInput struct {
		Body types.SearchRequest `doc:"Search query"`
	}

	SearchSettingsOutput struct {
		Body types.SearchResponse
	}

	GetCategoriesOutput struct {
		Body []types.Category
	}
)

func (h *SettingsHandler) runtimeSettings(ctx context.Context, authenticated bool) types.SettingsRuntime {
	telemetryRuntime := types.SettingsRuntime{
		FrontendTracingEnabled: !h.cfg.AgentEnabled && telemetry.Endpoint("traces") != "",
		FrontendMetricsEnabled: !h.cfg.AgentEnabled && telemetry.Endpoint("metrics") != "",
		FrontendLogsEnabled:    !h.cfg.AgentEnabled && telemetry.Endpoint("logs") != "",
	}
	if !authenticated {
		return telemetryRuntime
	}

	_, agentMTLSCAErr := h.mtls.CAPath(ctx)
	settingsCfg := h.settingsService.GetSettingsConfig()
	return types.SettingsRuntime{
		FrontendTracingEnabled:      telemetryRuntime.FrontendTracingEnabled,
		FrontendMetricsEnabled:      telemetryRuntime.FrontendMetricsEnabled,
		FrontendLogsEnabled:         telemetryRuntime.FrontendLogsEnabled,
		BackupVolumeName:            kit.Ternary(strings.TrimSpace(h.cfg.BackupVolumeName) != "", h.cfg.BackupVolumeName, "arcane-backups"),
		AgentMTLSManagerCAAvailable: agentMTLSCAErr == nil,
		DepotConfigured:             strings.TrimSpace(settingsCfg.DepotProjectId.Value) != "" && strings.TrimSpace(settingsCfg.DepotToken.Value) != "",
	}
}

func (h *SettingsHandler) GetPublicSettings(ctx context.Context, _ *types.EnvironmentInput) (*SettingsSnapshotOutput, error) {
	_, authenticated := middleware.GetUserIDFromContext(ctx)
	snapshot := h.settingsService.SettingsSnapshot(kit.Ternary(authenticated, SettingVisibilityNonAdmin, SettingVisibilityPublic))
	return &SettingsSnapshotOutput{Body: types.SettingsSnapshot{
		Settings: snapshot.Settings,
		Runtime:  h.runtimeSettings(ctx, false),
		ReadOnly: snapshot.ReadOnly,
	}}, nil
}

func (h *SettingsHandler) GetEnvironmentSettings(ctx context.Context, input *types.EnvironmentInput) (*SettingsSnapshotOutput, error) {
	if input.EnvironmentID != "0" {
		snapshot, err := h.proxyRemoteJSON.JSON[types.SettingsSnapshot](ctx, input.EnvironmentID, http.MethodGet, "/api/agents/0/settings", nil)
		if err != nil {
			return nil, err
		}
		return &SettingsSnapshotOutput{Body: *snapshot}, nil
	}
	return h.scopedSnapshot(ctx, SettingScopeEnvironment), nil
}

func (h *SettingsHandler) UpdateEnvironmentSettings(ctx context.Context, input *UpdateSettingsInput) (*SettingsSnapshotOutput, error) {
	if err := h.settingsService.ValidateScope(SettingScopeEnvironment, input.Body); err != nil {
		return nil, handlerutil.APIError(err)
	}
	if input.EnvironmentID != "0" {
		if _, err := h.proxyRemoteJSON.JSON[struct{}](ctx, input.EnvironmentID, http.MethodPatch, "/api/agents/0/settings", input.Body); err != nil {
			return nil, err
		}
		return h.GetEnvironmentSettings(ctx, &types.EnvironmentInput{EnvironmentID: input.EnvironmentID})
	}
	if err := h.updateLocal(ctx, input.Body); err != nil {
		return nil, err
	}
	return h.scopedSnapshot(ctx, SettingScopeEnvironment), nil
}

func (h *SettingsHandler) GetManagerSettings(ctx context.Context, _ *struct{}) (*SettingsSnapshotOutput, error) {
	return h.scopedSnapshot(ctx, SettingScopeManager), nil
}

func (h *SettingsHandler) UpdateManagerSettings(ctx context.Context, input *UpdateScopedSettingsInput) (*SettingsSnapshotOutput, error) {
	return h.updateScoped(ctx, SettingScopeManager, input.Body)
}

func (h *SettingsHandler) GetAuthenticationSettings(ctx context.Context, _ *struct{}) (*SettingsSnapshotOutput, error) {
	return h.scopedSnapshot(ctx, SettingScopeAuthentication), nil
}

func (h *SettingsHandler) UpdateAuthenticationSettings(ctx context.Context, input *UpdateScopedSettingsInput) (*SettingsSnapshotOutput, error) {
	return h.updateScoped(ctx, SettingScopeAuthentication, input.Body)
}

func (h *SettingsHandler) scopedSnapshot(ctx context.Context, scope SettingScope) *SettingsSnapshotOutput {
	snapshot := h.settingsService.SettingsSnapshot(SettingVisibilityAll, scope)
	return &SettingsSnapshotOutput{Body: types.SettingsSnapshot{
		Settings: snapshot.Settings,
		Runtime:  h.runtimeSettings(ctx, true),
		ReadOnly: snapshot.ReadOnly,
	}}
}

func (h *SettingsHandler) updateScoped(ctx context.Context, scope SettingScope, updates types.SettingsUpdate) (*SettingsSnapshotOutput, error) {
	if err := h.settingsService.ValidateScope(scope, updates); err != nil {
		return nil, handlerutil.APIError(err)
	}
	if err := h.updateLocal(ctx, updates); err != nil {
		return nil, err
	}
	return h.scopedSnapshot(ctx, scope), nil
}

func (h *SettingsHandler) updateLocal(ctx context.Context, updates types.SettingsUpdate) error {
	err := h.settingsService.UpdateSettings(ctx, updates)
	if err == nil {
		return nil
	}
	apiErr := common.ToAPIError(err)
	switch apiErr.HTTPStatus() {
	case http.StatusInternalServerError:
		return huma.Error500InternalServerError("Failed to update settings")
	case http.StatusBadRequest:
		return handlerutil.APIError(err)
	default:
		return huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
}

func (h *SettingsHandler) Search(ctx context.Context, input *SearchSettingsInput) (*SearchSettingsOutput, error) {
	if strings.TrimSpace(input.Body.Query) == "" {
		return nil, huma.Error400BadRequest("Query parameter is required")
	}

	categories, err := h.GetCategories(ctx, nil)
	if err != nil {
		return nil, err
	}
	results := search.Search(categories.Body, input.Body.Query)
	return &SearchSettingsOutput{Body: results}, nil
}

func (h *SettingsHandler) GetCategories(ctx context.Context, input *struct{}) (*GetCategoriesOutput, error) {
	categories := []types.Category{}
	ps, _ := middleware.PermissionsFromContext(ctx)
	if ps == nil {
		return &GetCategoriesOutput{Body: categories}, nil
	}
	scopes := append([]string{""}, slices.Collect(maps.Keys(ps.PerEnv))...)
	for _, cat := range h.settingsSearchService.GetSettingsCategories() {
		if slices.ContainsFunc(scopes, func(envID string) bool { return authz.CanAccessSurface(ps, "settings.category."+cat.ID, envID) }) {
			categories = append(categories, cat)
		}
	}
	return &GetCategoriesOutput{Body: categories}, nil
}
