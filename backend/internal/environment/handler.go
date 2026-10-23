package environment

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/streams/agg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/remenv"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
)

const (
	// Re-applies runtime state to cached rows to cover poll-mode TTL expiry.
	environmentStreamRuntimeInterval = 5 * time.Second
	// Safety net for writes that bypass the runtime-change signal.
	environmentStreamReloadInterval = 60 * time.Second
	environmentStreamRefreshFloor   = 30 * time.Second
	environmentStreamHubKey         = "environments"

	remoteChangeReconcileInterval = 30 * time.Second
	remoteChangeMinBackoff        = time.Second
	remoteChangeMaxBackoff        = 30 * time.Second
	remoteChangeUnsupportedDelay  = 60 * time.Second
	remoteChangeWaitMargin        = 5 * time.Second
)

// EnvironmentHandler handles environment management endpoints.
type EnvironmentHandler struct {
	environmentService *EnvironmentService
	settingsService    *settings.SettingsService
	apiKeyService      *apikey.ApiKeyService
	eventService       *event.EventService
	cfg                *config.Config
	streamHub          *agg.Hub[[]types.Environment]
	changeHub          *agg.Hub[struct{}]
	changeCursors      *concurrency.StateMap[string, types.EnvironmentChanges]
	activityService    activity.Service
	appCtx             context.Context
	localVersion       func(context.Context) *types.VersionInfo
}

type ListEnvironmentsInput struct {
	Search string `query:"search" doc:"Search query for filtering by name or API URL"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	Type   string `query:"type" doc:"Filter by environment type (comma-separated: http,edge,websocket,grpc,polling)"`
}

type CreateEnvironmentInput struct {
	types.Create[types.Environment]
}

type GetEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type UpdateEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
	types.Update[types.Environment]
}

type DeleteEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type TestConnectionInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type SyncEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type PairEnvironmentInput struct {
	XAPIKey string `header:"X-API-Key" doc:"API key for environment pairing"`
}

type GetDeploymentSnippetsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentVersionInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type DownloadEdgeMTLSCAInput struct{}

type DownloadEnvironmentMTLSBundleInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type DownloadEnvironmentMTLSFileInput struct {
	ID       string `path:"id" doc:"Environment ID"`
	FileName string `path:"fileName" doc:"mTLS asset filename"`
}

// NewHandler builds the environment HTTP handler and its stream producer.
func NewHandler(
	environmentService *EnvironmentService,
	settingsService *settings.SettingsService,
	apiKeyService *apikey.ApiKeyService,
	eventService *event.EventService,
	cfg *config.Config,
	activityService activity.Service,
) *EnvironmentHandler {
	return &EnvironmentHandler{
		environmentService: environmentService,
		settingsService:    settingsService,
		apiKeyService:      apiKeyService,
		eventService:       eventService,
		cfg:                cfg,
		streamHub:          agg.NewHub[[]types.Environment](),
		changeHub:          agg.NewHub[struct{}](),
		changeCursors:      concurrency.NewStateMap[string, types.EnvironmentChanges](),
		activityService:    activityService,
	}
}

// ListEnvironments returns a paginated list of environments.
func (h *EnvironmentHandler) ListEnvironments(ctx context.Context, input *ListEnvironmentsInput) (*handlerutil.Page[types.Environment], error) {
	ps, ok := middleware.PermissionsFromContext(ctx)
	if !ok {
		return nil, huma.Error403Forbidden("permission denied")
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	page, err := h.environmentService.ListAccessibleEnvironments(ctx, ps, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch environments")
	}

	return &handlerutil.Page[types.Environment]{Body: page}, nil
}

func (h *EnvironmentHandler) RunStreamProducer(ctx context.Context, ps *authz.PermissionSet, events chan<- types.EnvironmentStreamEvent) {
	var lastFingerprint uint64
	var haveFingerprint bool
	var lastSentAt time.Time
	accessibleEnvIDs := ps.AccessibleEnvironmentIDs()

	h.streamHub.Subscribe(ctx, environmentStreamHubKey, func(ctx context.Context, publish func([]types.Environment)) {
		changes, unsubscribe := h.environmentService.SubscribeRuntimeChanges()
		defer unsubscribe()

		var rows []types.Environment
		loaded := false
		publishRuntime := func() {
			if !loaded {
				return
			}
			envs := make([]types.Environment, 0, len(rows))
			for _, row := range rows {
				envs = append(envs, ApplyEnvironmentRuntimeState(row))
			}
			publish(envs)
		}
		reload := func() {
			envs, err := h.environmentService.ListVisibleEnvironments(ctx)
			if err != nil {
				// A failed read must not end the stream; the next wake-up retries.
				if ctx.Err() == nil {
					slog.WarnContext(ctx, "environment stream failed to list environments", "error", err)
				}
				return
			}
			rows = envs
			loaded = true
			publishRuntime()
		}

		reload()

		runtimeTicker := time.NewTicker(environmentStreamRuntimeInterval)
		defer runtimeTicker.Stop()
		reloadTicker := time.NewTicker(environmentStreamReloadInterval)
		defer reloadTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				reload()
			case <-reloadTicker.C:
				reload()
			case <-runtimeTicker.C:
				if !loaded {
					reload()
					continue
				}
				publishRuntime()
			}
		}
	}, func(all []types.Environment) bool {
		// all is shared across subscribers, so filter a copy.
		envs := all
		if accessibleEnvIDs != nil {
			envs = slices.DeleteFunc(slices.Clone(all), func(env types.Environment) bool { return !slices.Contains(accessibleEnvIDs, env.ID) })
		}
		fingerprint := kit.Fingerprint(envs)
		// Re-send unchanged state on a floor so relative UI timestamps keep advancing.
		if haveFingerprint && fingerprint == lastFingerprint && time.Since(lastSentAt) < environmentStreamRefreshFloor {
			return true
		}
		lastFingerprint = fingerprint
		haveFingerprint = true
		lastSentAt = time.Now()

		return agg.Send(ctx, events, types.EnvironmentStreamEvent{
			Type:         "snapshot",
			Environments: envs,
			Timestamp:    time.Now(),
		})
	})
}

func (h *EnvironmentHandler) RunRemoteChangePollers(ctx context.Context, ps *authz.PermissionSet) {
	accessible := ps.AccessibleEnvironmentIDs()
	agg.ReconcilePollersByKey(ctx,
		func(ctx context.Context) ([]Environment, error) {
			environments, err := h.environmentService.ListActiveRemoteEnvironments(ctx)
			if err != nil {
				return nil, err
			}
			if _, removeErr := h.changeCursors.RemoveWhere(ctx, "environment change cursor", func(environmentID string, _ types.EnvironmentChanges) bool {
				return !slices.ContainsFunc(environments, func(environment Environment) bool {
					return environment.ID == environmentID
				})
			}); removeErr != nil {
				return nil, removeErr
			}
			return slices.DeleteFunc(environments, func(environment Environment) bool {
				return accessible != nil && !slices.Contains(accessible, environment.ID)
			}), nil
		},
		func(environment Environment) string {
			return environment.ID
		},
		ConnectionKey,
		remoteChangeReconcileInterval,
		"environment changes",
		func(pollCtx context.Context, environment Environment) {
			h.changeHub.Subscribe(pollCtx, ConnectionKey(environment),
				func(runCtx context.Context, _ func(struct{})) {
					h.runRemoteChangePoller(runCtx, environment)
				},
				func(struct{}) bool {
					return true
				})
		})
}

func ConnectionKey(environment Environment) string {
	return strings.Join([]string{
		environment.ID,
		environment.ApiUrl,
		kit.SHA256Hex(kit.FromPtr(environment.AccessToken)),
		strconv.FormatBool(environment.Enabled),
	}, "|")
}

func (h *EnvironmentHandler) runRemoteChangePoller(ctx context.Context, environment Environment) {
	var delay time.Duration
	for {
		if delay > 0 {
			wait := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				wait.Stop()
				return
			case <-wait.C:
			}
		}

		previous, _ := h.changeCursors.Get(environment.ID)
		var since uint64
		for _, revision := range previous.Kinds {
			since = max(since, revision)
		}
		var out types.ApiResponse[types.EnvironmentChanges]
		err := h.environmentService.ProxyJSONRequestForEnvironment(ctx, environment, http.MethodGet,
			"/api/environments/0/events/changes?"+url.Values{
				"epoch": {previous.Epoch},
				"since": {strconv.FormatUint(since, 10)},
				"wait":  {strconv.Itoa(max(int((timeouts.GetDuration(h.settingsService.GetSettingsConfig().ProxyRequestTimeout.AsInt(), timeouts.DefaultProxyRequest) - remoteChangeWaitMargin).Seconds()), 1))},
			}.Encode(), nil, &out)
		statusError, isStatusError := errors.AsType[*remenv.StatusError](err)
		_, isDecodeError := errors.AsType[*remenv.DecodeError](err)
		switch {
		case ctx.Err() != nil:
		case err == nil:
			h.eventService.RelayChanges(environment.ID, previous, out.Data)
			if _, _, storeErr := h.changeCursors.Store(ctx, "environment change cursor", environment.ID, out.Data); storeErr != nil {
				slog.WarnContext(ctx, "Failed to store environment change cursor", "environmentId", environment.ID, "error", storeErr)
				break
			}
			delay = 0
		case isDecodeError || isStatusError && statusError.StatusCode == http.StatusNotFound:
			delay = remoteChangeUnsupportedDelay
		default:
			delay = min(max(delay*2, remoteChangeMinBackoff), remoteChangeMaxBackoff)
			slog.DebugContext(ctx, "environment change poll failed", "environmentId", environment.ID, "error", err)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// CreateEnvironment creates a new environment.
func (h *EnvironmentHandler) CreateEnvironment(ctx context.Context, input *CreateEnvironmentInput) (*handlerutil.Out[types.Environment], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid environment body")
	}

	created, err := h.environmentService.CreateEnvironment(ctx, &Environment{
		Name:    input.Body.Name,
		Status:  string(EnvironmentStatusPending),
		Enabled: !submitted["enabled"] || input.Body.Enabled,
	}, &user.ID, &user.Username)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	created, apiKey, err := func() (*Environment, *string, error) {
		apiKeyDto, keyErr := h.apiKeyService.CreateEnvironmentApiKey(ctx, created.ID)
		if keyErr != nil {
			slog.ErrorContext(ctx, "Failed to create environment API key", "environmentId", created.ID, "error", keyErr.Error())
			return nil, nil, huma.Error500InternalServerError("Failed to create environment API key")
		}

		// The full key doubles as the manager-to-agent access token.
		updated, updateErr := h.environmentService.UpdateEnvironment(ctx, created.ID, map[string]any{
			"api_key_id":   apiKeyDto.ID,
			"access_token": apiKeyDto.Key,
		}, &user.ID, &user.Username)
		if updateErr != nil {
			// ErrApiKeyProtected means the link landed, so the key belongs to the environment and must survive.
			if delErr := h.apiKeyService.DeleteApiKey(ctx, apiKeyDto.ID); delErr != nil &&
				!errors.Is(delErr, apikey.ErrApiKeyNotFound) && !errors.Is(delErr, apikey.ErrApiKeyProtected) {
				slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentId", created.ID, "error", delErr.Error())
			}
			slog.ErrorContext(ctx, "Failed to link API key to environment", "environmentId", created.ID, "error", updateErr.Error())
			return nil, nil, huma.Error500InternalServerError("Failed to link API key")
		}
		return updated, new(apiKeyDto.Key), nil
	}()
	if err != nil {
		return nil, err
	}

	out, mapErr := mapping.MapOne[*Environment, types.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}

	out = ApplyEnvironmentRuntimeState(out)
	out.ApiKey = apiKey // keep: optional field on a 22-field mapped DTO

	return &handlerutil.Out[types.Environment]{
		Body: types.ApiResponse[types.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetEnvironment returns an environment by ID.
func (h *EnvironmentHandler) GetEnvironment(ctx context.Context, input *GetEnvironmentInput) (*handlerutil.Out[types.Environment], error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	out, mapErr := mapping.MapOne[*Environment, types.Environment](env)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	out = ApplyEnvironmentRuntimeState(out)
	if env.ID != LocalEnvironmentID {
		out.EdgeMTLSCertificate = func() *types.EdgeMTLSCertificate { // keep: optional field on a 22-field mapped DTO
			edgeCfg := h.edgeMTLSConfig()
			if edge.NormalizeEdgeMTLSMode(edgeCfg.EdgeMTLSMode) == edge.EdgeMTLSModeDisabled {
				return nil
			}
			certPath, pathErr := edge.GeneratedManagerClientMTLSCertPath(edgeCfg, env.ID)
			if pathErr != nil {
				return nil
			}
			// os rather than acfs: the assets dir may be configured anywhere on the host.
			certPEM, readErr := os.ReadFile(certPath)
			if readErr != nil {
				return nil
			}
			block, _ := pem.Decode(certPEM)
			if block == nil {
				return nil
			}
			cert, parseErr := x509.ParseCertificate(block.Bytes)
			if parseErr != nil {
				return nil
			}

			expiresAt := cert.NotAfter.UTC()
			now := time.Now().UTC()
			return &types.EdgeMTLSCertificate{
				CommonName:    kit.Ternary(strings.TrimSpace(cert.Subject.CommonName) != "", new(strings.TrimSpace(cert.Subject.CommonName)), nil),
				ExpiresAt:     &expiresAt,
				DaysRemaining: new(int(math.Ceil(max(expiresAt.Sub(now), 0).Hours() / 24))),
				Expired:       now.After(expiresAt),
				ExpiringSoon:  now.Before(expiresAt) && expiresAt.Sub(now) <= edgeMTLSCertificateExpiryWarningWindow,
			}
		}()
	}

	return &handlerutil.Out[types.Environment]{
		Body: types.ApiResponse[types.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateEnvironment updates an environment.
func (h *EnvironmentHandler) UpdateEnvironment(ctx context.Context, input *UpdateEnvironmentInput) (*handlerutil.Out[types.Environment], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("Invalid environment update body")
	}
	remote := input.ID != LocalEnvironmentID
	writable := map[string]bool{
		"name":    submitted["name"],
		"enabled": remote && submitted["enabled"],
	}
	updates := map[string]any{
		"name":    input.Body.Name,
		"enabled": input.Body.Enabled,
	}
	maps.DeleteFunc(updates, func(column string, _ any) bool {
		return !writable[column]
	})

	userID, username := currentUserRefs(ctx)
	updated, updateErr := h.environmentService.UpdateEnvironment(ctx, input.ID, updates, userID, username)
	if updateErr != nil {
		apiErr := common.ToAPIError(updateErr)
		if apiErr.HTTPStatus() == http.StatusInternalServerError {
			return nil, huma.Error500InternalServerError("Failed to update environment")
		}
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	if updated.Enabled {
		go func(syncCtx context.Context, envID, envName string) {
			status, testErr := h.environmentService.TestConnection(syncCtx, envID)
			if testErr != nil {
				slog.WarnContext(syncCtx, "Failed to test connection after environment update",
					"environmentId", envID, "environmentName", envName, "status", status, "error", testErr)
			}
		}(context.WithoutCancel(ctx), input.ID, updated.Name)
	}

	updated, newApiKey, err := func() (*Environment, *string, error) {
		if !input.Body.RegenerateApiKey {
			return updated, nil, nil
		}
		localUser, userErr := handlerutil.RequireUser(ctx)
		if userErr != nil {
			return nil, nil, userErr
		}

		apiKey, regenerateErr := h.environmentService.RegenerateEnvironmentApiKey(ctx, updated, localUser.ID, localUser.Username)
		if regenerateErr != nil {
			slog.ErrorContext(ctx, "Failed to regenerate API key", "environmentId", input.ID, "error", regenerateErr.Error())
			return nil, nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		refreshed, refreshErr := h.environmentService.GetEnvironmentByID(ctx, input.ID)
		if refreshErr != nil {
			slog.ErrorContext(ctx, "Failed to fetch updated environment", "environmentId", input.ID, "error", refreshErr.Error())
			return nil, nil, huma.Error500InternalServerError("Failed to fetch updated environment")
		}
		return refreshed, new(apiKey), nil
	}()
	if err != nil {
		return nil, err
	}

	out, mapErr := mapping.MapOne[*Environment, types.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	out = ApplyEnvironmentRuntimeState(out)
	out.ApiKey = newApiKey // keep: optional field on a 22-field mapped DTO

	return &handlerutil.Out[types.Environment]{
		Body: types.ApiResponse[types.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// currentUserRefs returns the request user's ID and username, or nils without a user.
func currentUserRefs(ctx context.Context) (userID, username *string) {
	if user, _ := userctx.CurrentUserFromContext(ctx); user != nil {
		return new(user.ID), new(user.Username)
	}
	return nil, nil
}

// DeleteEnvironment deletes an environment.
func (h *EnvironmentHandler) DeleteEnvironment(ctx context.Context, input *DeleteEnvironmentInput) (*handlerutil.Out[types.MessageResponse], error) {
	if input.ID == LocalEnvironmentID {
		return nil, huma.Error400BadRequest("Cannot delete local environment")
	}

	userID, username := currentUserRefs(ctx)
	if err := h.environmentService.DeleteEnvironment(ctx, input.ID, userID, username); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete environment: "+apiErr.Message)
	}

	return handlerutil.MessageOutput("Environment deleted successfully", ""), nil
}

// TestConnection tests connectivity to an environment.
func (h *EnvironmentHandler) TestConnection(ctx context.Context, input *TestConnectionInput) (*handlerutil.Out[types.EnvironmentTest], error) {
	status, err := h.environmentService.TestConnection(ctx, input.ID)
	if errors.Is(err, ErrEnvironmentNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return &handlerutil.Out[types.EnvironmentTest]{
			Body: types.ApiResponse[types.EnvironmentTest]{
				Success: false,
				Data:    types.EnvironmentTest{Status: status, Message: new(err.Error())},
			},
		}, err
	}

	return &handlerutil.Out[types.EnvironmentTest]{
		Body: types.ApiResponse[types.EnvironmentTest]{
			Success: true,
			Data:    types.EnvironmentTest{Status: status},
		},
	}, nil
}

// SyncEnvironment syncs manager-owned resources to an environment.
func (h *EnvironmentHandler) SyncEnvironment(ctx context.Context, input *SyncEnvironmentInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := h.environmentService.SyncResourcesToEnvironment(runtimeCtx, input.ID, user, h.activityService)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}

	return handlerutil.MessageOutput("Environment synced successfully", activityID), nil
}

func (h *EnvironmentHandler) triggerEnvironmentResourceSync(ctx context.Context, environmentID, environmentName, reason string) {
	h.environmentService.ForgetSyncState(environmentID)
	detachedCtx := context.WithoutCancel(ctx)

	for _, resource := range h.environmentService.resourceSyncs() {
		go func() {
			syncCtx, cancel := context.WithTimeout(detachedCtx, edge.DefaultProxyTimeout)
			defer cancel()
			if err := resource.sync(syncCtx, environmentID); err != nil {
				slog.WarnContext(syncCtx, "Failed to sync resources to environment",
					"kind", resource.kind, "environmentId", environmentID, "environmentName", environmentName, "reason", reason, "error", err.Error())
			}
		}()
	}
}

// PairEnvironment handles agent pairing callback with API key.
func (h *EnvironmentHandler) PairEnvironment(ctx context.Context, input *PairEnvironmentInput) (*handlerutil.Out[types.MessageResponse], error) {
	if input.XAPIKey == "" {
		return nil, huma.Error400BadRequest("X-API-Key header is required")
	}

	envID, err := h.apiKeyService.GetEnvironmentByApiKey(ctx, input.XAPIKey)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to validate API key for pairing", "error", err.Error())
		return nil, huma.Error401Unauthorized("Invalid API key")
	}

	if envID == nil {
		return nil, huma.Error400BadRequest("API key is not linked to an environment")
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, *envID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get environment", "environmentId", *envID, "error", err.Error())
		return nil, huma.Error404NotFound("Environment not found")
	}

	if env.Status != string(EnvironmentStatusPending) {
		return nil, huma.Error400BadRequest("Environment is not in pending status")
	}

	updates := map[string]any{
		"status": string(EnvironmentStatusOnline),
	}
	_, err = h.environmentService.UpdateEnvironment(ctx, *envID, updates, nil, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update environment status", "environmentId", *envID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to complete pairing")
	}

	slog.InfoContext(ctx, "Environment pairing completed", "environmentId", *envID, "environmentName", env.Name)
	h.triggerEnvironmentResourceSync(ctx, *envID, env.Name, "environment pairing")

	return handlerutil.MessageOutput("Environment pairing completed successfully", ""), nil
}

// GetDeploymentSnippets returns deployment snippets for an environment.
func (h *EnvironmentHandler) GetDeploymentSnippets(ctx context.Context, input *GetDeploymentSnippetsInput) (*handlerutil.Out[types.DeploymentSnippet], error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	if env.ApiKeyID == nil {
		return nil, huma.Error400BadRequest("Environment does not have an API key configured")
	}

	if kit.FromPtr(env.AccessToken) == "" {
		return nil, huma.Error400BadRequest("Environment is missing access token")
	}

	snippets, err := h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), *env.AccessToken, h.edgeMTLSConfig())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate deployment snippets", "environmentId", input.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to generate deployment snippets")
	}

	return &handlerutil.Out[types.DeploymentSnippet]{
		Body: types.ApiResponse[types.DeploymentSnippet]{
			Success: true,
			Data: types.DeploymentSnippet{
				DockerRun:     snippets.DockerRun,
				DockerCompose: snippets.DockerCompose,
				MTLS: func() *types.DeploymentSnippetMTLS {
					if snippets.MTLS == nil {
						return nil
					}
					files := make([]types.DeploymentSnippetFile, 0, len(snippets.MTLS.Files))
					for _, file := range snippets.MTLS.Files {
						// Secret material is only served through the download endpoint.
						files = append(files, types.DeploymentSnippetFile{
							Name:          file.Name,
							Content:       kit.Ternary(isSensitiveMTLSAssetName(file.Name), "", file.Content),
							DownloadURL:   fmt.Sprintf("/api/environments/%s/deployment/mtls/%s", env.ID, file.Name),
							Sensitive:     isSensitiveMTLSAssetName(file.Name),
							ContainerPath: file.ContainerPath,
							Permissions:   file.Permissions,
						})
					}
					return &types.DeploymentSnippetMTLS{
						DockerRun:     snippets.MTLS.DockerRun,
						DockerCompose: snippets.MTLS.DockerCompose,
						Files:         files,
						HostDirHint:   snippets.MTLS.HostDirHint,
					}
				}(),
			},
		},
	}, nil
}

// GetEnvironmentVersion returns the version of the local or a remote environment.
func (h *EnvironmentHandler) GetEnvironmentVersion(ctx context.Context, input *GetEnvironmentVersionInput) (*handlerutil.Out[types.VersionInfo], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.environmentService.AggregateVersions(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get environment versions: " + err.Error())
		}
		return &handlerutil.Out[types.VersionInfo]{Body: body}, nil
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var versionInfo types.VersionInfo
	if env.ID == LocalEnvironmentID {
		versionInfo = *h.localVersion(reqCtx)
	} else if proxyErr := h.environmentService.ProxyJSONRequestForEnvironment(reqCtx, *env, http.MethodGet, "/api/app/version", nil, &versionInfo); proxyErr != nil {
		return nil, handlerutil.TranslateRemoteProxyError(proxyErr)
	}

	// A successful reply proves the environment is online.
	if updateErr := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); updateErr != nil {
		slog.WarnContext(ctx, "Failed to update environment heartbeat", "environmentId", input.ID, "error", updateErr)
	}

	return &handlerutil.Out[types.VersionInfo]{
		Body: types.ApiResponse[types.VersionInfo]{
			Success: true,
			Data:    versionInfo,
		},
	}, nil
}

// DownloadEdgeMTLSCA downloads the Arcane-managed edge mTLS CA certificate.
func (h *EnvironmentHandler) DownloadEdgeMTLSCA(ctx context.Context, _ *DownloadEdgeMTLSCAInput) (*huma.StreamResponse, error) {
	caPath, err := edge.AvailableManagerMTLSCAPath(h.edgeMTLSConfig())
	if err != nil {
		return nil, huma.Error404NotFound("Arcane-managed edge mTLS CA is not available")
	}

	// os rather than acfs: the CA path may be configured anywhere on the host.
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read generated edge mTLS CA", "path", caPath, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to read Arcane-generated edge mTLS CA")
	}

	fileName := cmp.Or(strings.TrimSpace(filepath.Base(caPath)), "ca.crt")
	return h.mtlsDownloadResponse(nil, "application/x-pem-file", fileName, caPEM,
		"mTLS CA downloaded", fmt.Sprintf("Administrator downloaded edge mTLS CA %q", fileName),
		database.JSON{"fileName": fileName, "kind": "ca"}), nil
}

func (h *EnvironmentHandler) DownloadEnvironmentMTLSBundle(ctx context.Context, input *DownloadEnvironmentMTLSBundleInput) (*huma.StreamResponse, error) {
	env, files, err := h.loadEnvironmentMTLSFiles(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)

	for _, file := range files {
		downloadName := environmentMTLSAssetDownloadName(env, file.Name)
		header := &zip.FileHeader{
			Name:   downloadName,
			Method: zip.Deflate,
		}
		header.SetMode(func() os.FileMode {
			if parsed, parseErr := strconv.ParseUint(strings.TrimSpace(file.Permissions), 8, 32); parseErr == nil && parsed != 0 {
				return os.FileMode(parsed)
			}
			return kit.Ternary[os.FileMode](isSensitiveMTLSAssetName(file.Name), 0o600, 0o644)
		}())

		entry, createErr := zipWriter.CreateHeader(header)
		if createErr != nil {
			slog.ErrorContext(ctx, "Failed to create mTLS bundle entry", "environmentId", input.ID, "fileName", downloadName, "error", createErr.Error())
			return nil, huma.Error500InternalServerError("Failed to build mTLS bundle")
		}

		if _, writeErr := entry.Write([]byte(file.Content)); writeErr != nil {
			slog.ErrorContext(ctx, "Failed to write mTLS bundle entry", "environmentId", input.ID, "fileName", downloadName, "error", writeErr.Error())
			return nil, huma.Error500InternalServerError("Failed to build mTLS bundle")
		}
	}

	if closeErr := zipWriter.Close(); closeErr != nil {
		slog.ErrorContext(ctx, "Failed to finalize mTLS bundle", "environmentId", input.ID, "error", closeErr.Error())
		return nil, huma.Error500InternalServerError("Failed to build mTLS bundle")
	}

	fileName := environmentMTLSDownloadBaseName(env) + "-mtls.zip"
	return h.mtlsDownloadResponse(env, "application/zip", fileName, archive.Bytes(),
		"mTLS bundle downloaded", fmt.Sprintf("Administrator downloaded edge mTLS bundle %q (%d files)", fileName, len(files)),
		database.JSON{"fileName": fileName, "kind": "bundle", "fileCount": len(files)}), nil
}

func (h *EnvironmentHandler) DownloadEnvironmentMTLSFile(ctx context.Context, input *DownloadEnvironmentMTLSFileInput) (*huma.StreamResponse, error) {
	env, files, err := h.loadEnvironmentMTLSFiles(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(files, func(file types.DeploymentSnippetFile) bool { return file.Name == input.FileName })
	if index < 0 {
		return nil, huma.Error404NotFound("Requested mTLS asset was not found")
	}
	file := files[index]

	return h.mtlsDownloadResponse(env, "application/x-pem-file", environmentMTLSAssetDownloadName(env, file.Name), []byte(file.Content),
		"mTLS asset downloaded", fmt.Sprintf("Administrator downloaded edge mTLS asset %q", file.Name),
		database.JSON{"fileName": file.Name, "kind": "file", "sensitive": isSensitiveMTLSAssetName(file.Name)}), nil
}

// edgeMTLSConfig maps the handler config to edge mTLS settings, or nil without config.
func (h *EnvironmentHandler) edgeMTLSConfig() *edge.Config {
	return &edge.Config{
		EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
		EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
		AppURL:            h.cfg.GetAppURL(),
	}
}

func (h *EnvironmentHandler) loadEnvironmentMTLSFiles(ctx context.Context, environmentID string) (*Environment, []types.DeploymentSnippetFile, error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return nil, nil, huma.Error404NotFound("Environment not found")
	}
	if env.ID == LocalEnvironmentID {
		return nil, nil, huma.Error400BadRequest("Environment is not an edge agent")
	}
	if env.ApiKeyID == nil {
		return nil, nil, huma.Error400BadRequest("Environment does not have an API key configured")
	}
	if kit.FromPtr(env.AccessToken) == "" {
		return nil, nil, huma.Error400BadRequest("Environment is missing access token")
	}

	snippets, err := h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), *env.AccessToken, h.edgeMTLSConfig())
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate environment mTLS assets", "environmentId", environmentID, "error", err.Error())
		return nil, nil, huma.Error500InternalServerError("Failed to generate environment mTLS assets")
	}

	if snippets.MTLS == nil || len(snippets.MTLS.Files) == 0 {
		return nil, nil, huma.Error404NotFound("mTLS assets are not available for this environment")
	}

	return env, snippets.MTLS.Files, nil
}

// mtlsDownloadResponse streams an mTLS asset and audits the download; the audit
// must never include certificate or key material.
func (h *EnvironmentHandler) mtlsDownloadResponse(env *Environment, contentType, fileName string, content []byte, title, description string, metadata database.JSON) *huma.StreamResponse {
	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			ctx := humaCtx.Context()
			humaCtx.SetHeader("Content-Type", contentType)
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			humaCtx.SetHeader("Content-Length", strconv.Itoa(len(content)))

			if written, writeErr := humaCtx.BodyWriter().Write(content); writeErr != nil || written != len(content) {
				slog.WarnContext(ctx, "Failed to stream edge mTLS download", "fileName", fileName, "bytesWritten", written, "bytesExpected", len(content), "error", writeErr)
				return
			}

			if remoteAddr := strings.TrimSpace(middleware.GetRemoteAddrFromContext(ctx)); remoteAddr != "" {
				metadata["remoteAddr"] = remoteAddr
			}
			userID, username := currentUserRefs(ctx)
			if _, err := h.eventService.CreateEvent(ctx, event.CreateEventRequest{
				Type:          event.EventTypeEnvironmentMTLSDownload,
				Severity:      event.EventSeverityInfo,
				Title:         title,
				Description:   description,
				ResourceType:  kit.Ternary(env != nil, new("environment"), nil),
				ResourceID:    kit.Ternary(env != nil, new(kit.FromPtr(env).ID), nil),
				ResourceName:  kit.Ternary(env != nil, new(kit.FromPtr(env).Name), nil),
				UserID:        userID,
				Username:      username,
				EnvironmentID: kit.Ternary(env != nil, new(kit.FromPtr(env).ID), nil),
				Metadata:      metadata,
			}); err != nil {
				slog.WarnContext(ctx, "Failed to record mTLS audit event", "type", string(event.EventTypeEnvironmentMTLSDownload), "error", err)
			}
		},
	}
}

// isSensitiveMTLSAssetName reports whether a generated asset holds secret material
// (the agent private key), which must never be returned inline.
func isSensitiveMTLSAssetName(fileName string) bool {
	name := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(name, ".key") || strings.HasSuffix(name, "-key.pem") || strings.HasSuffix(name, "_key.pem")
}

func environmentMTLSDownloadBaseName(env *Environment) string {
	baseName := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, strings.TrimSpace(env.Name))
	return cmp.Or(strings.Trim(baseName, "-"), "environment") + "-" + env.ID
}

func environmentMTLSAssetDownloadName(env *Environment, fileName string) string {
	switch fileName {
	case "agent.crt":
		return environmentMTLSDownloadBaseName(env) + ".pem"
	case "agent.key":
		return environmentMTLSDownloadBaseName(env) + ".key"
	default:
		return fileName
	}
}

const edgeMTLSCertificateExpiryWarningWindow = 30 * 24 * time.Hour
