package settings

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/search"
	"github.com/getarcaneapp/arcane/backend/v3/internal/telemetry"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// SettingsHandler provides Huma-based settings management endpoints.
type SettingsHandler struct {
	settingsService       *SettingsService
	settingsSearchService *SettingsSearchService
	proxyRemoteJSON       handlerutil.RemoteJSONProxy
	cfg                   *config.Config
}

type SettingsSnapshotOutput struct {
	Body types.SettingsSnapshot
}

type UpdateSettingsInput struct {
	EnvironmentID string               `path:"id" doc:"Environment ID"`
	Body          types.SettingsUpdate `doc:"Setting keys mapped to their new values"`
}

type SearchSettingsInput struct {
	Body types.SearchRequest `doc:"Search query"`
}

type SearchSettingsOutput struct {
	Body types.SearchResponse
}

type GetCategoriesOutput struct {
	Body []types.Category
}

func (h *SettingsHandler) runtimeSettings(authenticated bool) types.SettingsRuntime {
	telemetryRuntime := types.SettingsRuntime{
		FrontendTracingEnabled: !h.cfg.AgentMode && telemetry.Endpoint("traces") != "",
		FrontendMetricsEnabled: !h.cfg.AgentMode && telemetry.Endpoint("metrics") != "",
		FrontendLogsEnabled:    !h.cfg.AgentMode && telemetry.Endpoint("logs") != "",
	}
	if !authenticated {
		return telemetryRuntime
	}

	_, edgeMTLSCAErr := edge.AvailableManagerMTLSCAPath(&edge.Config{
		EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
		EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
	})
	settingsCfg := h.settingsService.GetSettingsConfig()
	return types.SettingsRuntime{
		FrontendTracingEnabled:     telemetryRuntime.FrontendTracingEnabled,
		FrontendMetricsEnabled:     telemetryRuntime.FrontendMetricsEnabled,
		FrontendLogsEnabled:        telemetryRuntime.FrontendLogsEnabled,
		BackupVolumeName:           kit.Ternary(strings.TrimSpace(h.cfg.BackupVolumeName) != "", h.cfg.BackupVolumeName, "arcane-backups"),
		EdgeMTLSManagerCAAvailable: edgeMTLSCAErr == nil,
		DepotConfigured:            strings.TrimSpace(settingsCfg.DepotProjectId.Value) != "" && strings.TrimSpace(settingsCfg.DepotToken.Value) != "",
	}
}

// GetPublicSettings returns public settings for an environment.
func (h *SettingsHandler) GetPublicSettings(ctx context.Context, input *types.EnvironmentInput) (*SettingsSnapshotOutput, error) {
	if input.EnvironmentID != "0" {
		snapshot, err := h.proxyRemoteJSON.JSON[types.SettingsSnapshot](ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings/public", nil)
		if err != nil {
			return nil, err
		}
		return &SettingsSnapshotOutput{Body: *snapshot}, nil
	}

	snapshot := h.settingsService.SettingsSnapshot(SettingVisibilityPublic)
	return &SettingsSnapshotOutput{Body: types.SettingsSnapshot{
		Settings: snapshot.Settings,
		Runtime:  h.runtimeSettings(false),
		ReadOnly: snapshot.ReadOnly,
	}}, nil
}

// GetSettings returns all settings for an environment.
func (h *SettingsHandler) GetSettings(ctx context.Context, input *types.EnvironmentInput) (*SettingsSnapshotOutput, error) {
	ps, _ := middleware.PermissionsFromContext(ctx)
	isAdmin := ps.IsGlobalAdmin()

	if input.EnvironmentID != "0" {
		snapshot, err := h.proxyRemoteJSON.JSON[types.SettingsSnapshot](ctx, input.EnvironmentID, http.MethodGet, "/api/environments/0/settings", nil)
		if err != nil {
			return nil, err
		}
		if isAdmin {
			return &SettingsSnapshotOutput{Body: *snapshot}, nil
		}
		_, byKey := settingsFields()
		return &SettingsSnapshotOutput{Body: types.SettingsSnapshot{
			Settings: slices.DeleteFunc(snapshot.Settings, func(setting types.SettingState) bool {
				return !byKey[setting.Key].isVisibleToNonAdmin
			}),
			Runtime:  snapshot.Runtime,
			ReadOnly: snapshot.ReadOnly,
		}}, nil
	}

	snapshot := h.settingsService.SettingsSnapshot(kit.Ternary(isAdmin, SettingVisibilityAll, SettingVisibilityNonAdmin))
	return &SettingsSnapshotOutput{Body: types.SettingsSnapshot{
		Settings: snapshot.Settings,
		Runtime:  h.runtimeSettings(true),
		ReadOnly: snapshot.ReadOnly,
	}}, nil
}

// UpdateSettings updates settings for an environment.
func (h *SettingsHandler) UpdateSettings(ctx context.Context, input *UpdateSettingsInput) (*SettingsSnapshotOutput, error) {
	if input.EnvironmentID != "0" {
		_, byKey := settingsFields()
		for key := range input.Body {
			if byKey[key].isManagerOnly {
				return nil, huma.Error403Forbidden(key + " can only be updated from the main environment")
			}
		}
		if _, err := h.proxyRemoteJSON.JSON[struct{}](ctx, input.EnvironmentID, http.MethodPatch, "/api/environments/0/settings", input.Body); err != nil {
			return nil, err
		}
		return h.GetSettings(ctx, &types.EnvironmentInput{EnvironmentID: input.EnvironmentID})
	}

	if err := h.settingsService.UpdateSettings(ctx, input.Body); err != nil {
		apiErr := common.ToAPIError(err)
		switch apiErr.HTTPStatus() {
		case http.StatusInternalServerError:
			return nil, huma.Error500InternalServerError("Failed to update settings")
		case http.StatusBadRequest:
			return nil, &struct {
				*huma.ErrorModel

				Details any `json:"details,omitempty"`
			}{
				ErrorModel: &huma.ErrorModel{
					Title:  http.StatusText(http.StatusBadRequest),
					Status: http.StatusBadRequest,
					Detail: apiErr.Message,
				},
				Details: apiErr.Details,
			}
		default:
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
	}
	return h.GetSettings(ctx, &types.EnvironmentInput{EnvironmentID: input.EnvironmentID})
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
	results := search.Search(categories.Body, input.Body.Query, types.SearchProfileSettings)
	if results.Results == nil {
		results.Results = []types.Category{}
	}
	return &SearchSettingsOutput{Body: results}, nil
}

// GetCategories returns the settings categories reachable at any scope.
func (h *SettingsHandler) GetCategories(ctx context.Context, input *struct{}) (*GetCategoriesOutput, error) {
	categories := []types.Category{}
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
