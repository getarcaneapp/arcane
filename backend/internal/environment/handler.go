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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/getarcaneapp/arcane/types/v2/version"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/kit/pkg/mapping"
	"go.getarcane.app/streams/agg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

const (
	// Re-applies runtime state to cached rows to cover poll-mode TTL expiry.
	environmentStreamRuntimeInterval = 5 * time.Second
	// Safety net for writes that bypass the runtime-change signal.
	environmentStreamReloadInterval = 60 * time.Second
	environmentStreamRefreshFloor   = 30 * time.Second
	environmentStreamHubKey         = "environments"
)

// EnvironmentHandler handles environment management endpoints.
type EnvironmentHandler struct {
	environmentService *EnvironmentService
	settingsService    *settings.SettingsService
	apiKeyService      *apikey.ApiKeyService
	eventService       *event.EventService
	cfg                *config.Config
	streamHub          *agg.Hub[[]environment.Environment]
	activityService    activity.Service
	appCtx             context.Context
	localVersion       func(context.Context) *version.Info
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
	Body environment.Create
}

type EnvironmentWithApiKey struct {
	environment.Environment

	ApiKey *string `json:"apiKey,omitempty" doc:"API key for pairing (only shown once during creation)"`
}

type GetEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type UpdateEnvironmentInput struct {
	ID   string `path:"id" doc:"Environment ID"`
	Body environment.Update
}

type DeleteEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type TestConnectionInput struct {
	ID   string                             `path:"id" doc:"Environment ID"`
	Body *environment.TestConnectionRequest `json:"body,omitempty"`
}

type UpdateHeartbeatInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type PairAgentInput struct {
	ID   string                        `path:"id" doc:"Environment ID (must be 0 for local)"`
	Body *environment.AgentPairRequest `json:"body,omitempty"`
}

type SyncEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type PairEnvironmentInput struct {
	XAPIKey string `header:"X-API-Key" doc:"API key for environment pairing"`
}

type DeploymentSnippet struct {
	DockerRun     string                 `json:"dockerRun" doc:"Docker run command snippet"`
	DockerCompose string                 `json:"dockerCompose" doc:"Docker compose YAML snippet"`
	MTLS          *DeploymentSnippetMTLS `json:"mtls,omitempty" doc:"Optional Arcane-generated mTLS deployment assets for edge agents"`
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
		streamHub:          agg.NewHub[[]environment.Environment](),
		activityService:    activityService,
	}
}

// ListEnvironments returns a paginated list of environments.
func (h *EnvironmentHandler) ListEnvironments(ctx context.Context, input *ListEnvironmentsInput) (*handlerutil.Page[environment.Environment], error) {
	ps, ok := middleware.PermissionsFromContext(ctx)
	if !ok {
		return nil, huma.Error403Forbidden("permission denied")
	}
	accessibleEnvIDs := accessibleEnvironmentIDs(ps)

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	envs, paginationResp, err := h.environmentService.ListEnvironmentsPaginated(ctx, params, accessibleEnvIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch environments")
	}
	for i := range envs {
		ApplyEnvironmentRuntimeState(&envs[i])
	}

	return &handlerutil.Page[environment.Environment]{
		Body: base.Paginated[environment.Environment]{
			Success:    true,
			Data:       envs,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// accessibleEnvironmentIDs returns nil when the caller may list every environment,
// otherwise the sorted IDs it holds at least one permission on.
func accessibleEnvironmentIDs(ps *authz.PermissionSet) []string {
	switch {
	case ps == nil:
		return []string{}
	case ps.Allows(authz.PermEnvironmentsList, ""):
		return nil
	default:
		// Never nil: callers treat nil as unrestricted, so a user without grants must get an empty list.
		ids := slices.AppendSeq(make([]string, 0, len(ps.PerEnv)), maps.Keys(ps.PerEnv))
		slices.Sort(ids)
		return ids
	}
}

func (h *EnvironmentHandler) RunStreamProducer(ctx context.Context, ps *authz.PermissionSet, events chan<- environment.StreamEvent) {
	var lastFingerprint uint64
	var haveFingerprint bool
	var lastSentAt time.Time
	accessibleEnvIDs := accessibleEnvironmentIDs(ps)

	h.streamHub.Subscribe(ctx, environmentStreamHubKey, h.runEnvironmentStreamLister, func(all []environment.Environment) bool {
		// all is shared across subscribers, so filter a copy.
		envs := all
		if accessibleEnvIDs != nil {
			envs = slices.DeleteFunc(slices.Clone(all), func(env environment.Environment) bool { return !slices.Contains(accessibleEnvIDs, env.ID) })
		}
		fingerprint := kit.Fingerprint(envs)
		// Re-send unchanged state on a floor so relative UI timestamps keep advancing.
		if haveFingerprint && fingerprint == lastFingerprint && time.Since(lastSentAt) < environmentStreamRefreshFloor {
			return true
		}
		lastFingerprint = fingerprint
		haveFingerprint = true
		lastSentAt = time.Now()

		return agg.Send(ctx, events, environment.StreamEvent{
			Type:         "snapshot",
			Environments: envs,
			Timestamp:    time.Now(),
		})
	})
}

func (h *EnvironmentHandler) runEnvironmentStreamLister(ctx context.Context, publish func([]environment.Environment)) {
	changes, unsubscribe := h.environmentService.SubscribeRuntimeChanges()
	defer unsubscribe()

	var rows []environment.Environment
	loaded := false
	publishRuntime := func() {
		if !loaded {
			return
		}
		envs := slices.Clone(rows)
		for i := range envs {
			ApplyEnvironmentRuntimeState(&envs[i])
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
}

// CreateEnvironment creates a new environment.
func (h *EnvironmentHandler) CreateEnvironment(ctx context.Context, input *CreateEnvironmentInput) (*handlerutil.Out[EnvironmentWithApiKey], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	env := &Environment{
		ApiUrl:  input.Body.ApiUrl,
		Enabled: true,
	}
	if input.Body.Name != nil {
		env.Name = *input.Body.Name
	}
	if input.Body.Enabled != nil {
		env.Enabled = *input.Body.Enabled
	}
	if input.Body.IsEdge != nil {
		env.IsEdge = *input.Body.IsEdge
	}
	useApiKey := input.Body.UseApiKey != nil && *input.Body.UseApiKey
	if useApiKey {
		env.Status = string(EnvironmentStatusPending)
	} else if input.Body.AccessToken != nil && *input.Body.AccessToken != "" {
		env.AccessToken = input.Body.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create environment: " + err.Error())
	}

	var apiKey *string
	if useApiKey {
		apiKeyDto, keyErr := h.apiKeyService.CreateEnvironmentApiKey(ctx, created.ID)
		if keyErr != nil {
			slog.ErrorContext(ctx, "Failed to create environment API key", "environmentId", created.ID, "error", keyErr.Error())
			return nil, huma.Error500InternalServerError("Failed to create environment API key")
		}

		// The full key doubles as the manager-to-agent access token.
		updates := map[string]any{
			"api_key_id":   apiKeyDto.ID,
			"access_token": apiKeyDto.Key,
		}
		updated, updateErr := h.environmentService.UpdateEnvironment(ctx, created.ID, updates, &user.ID, &user.Username)
		if updateErr != nil {
			// ErrApiKeyProtected means the link landed, so the key belongs to the environment and must survive.
			if delErr := h.apiKeyService.DeleteApiKey(ctx, apiKeyDto.ID); delErr != nil &&
				!errors.Is(delErr, apikey.ErrApiKeyNotFound) && !errors.Is(delErr, apikey.ErrApiKeyProtected) {
				slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentId", created.ID, "error", delErr.Error())
			}
			slog.ErrorContext(ctx, "Failed to link API key to environment", "environmentId", created.ID, "error", updateErr.Error())
			return nil, huma.Error500InternalServerError("Failed to link API key")
		}
		created = updated
		apiKey = new(apiKeyDto.Key)
	} else if created.AccessToken != nil && *created.AccessToken != "" {
		h.triggerEnvironmentResourceSync(ctx, created.ID, created.Name, "environment creation")
	}

	out, mapErr := mapping.MapOne[*Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	ApplyEnvironmentRuntimeState(&out)

	return &handlerutil.Out[EnvironmentWithApiKey]{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data:    EnvironmentWithApiKey{Environment: out, ApiKey: apiKey},
		},
	}, nil
}

// GetEnvironment returns an environment by ID.
func (h *EnvironmentHandler) GetEnvironment(ctx context.Context, input *GetEnvironmentInput) (*handlerutil.Out[environment.Environment], error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	out, mapErr := mapping.MapOne[*Environment, environment.Environment](env)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	ApplyEnvironmentRuntimeState(&out)
	if env.IsEdge {
		if certInfo, certErr := readGeneratedEdgeMTLSCertificateInfo(h.edgeMTLSConfig(), env.ID); certErr == nil {
			out.EdgeMTLSCertificate = certInfo
		}
	}

	return &handlerutil.Out[environment.Environment]{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateEnvironment updates an environment.
func (h *EnvironmentHandler) UpdateEnvironment(ctx context.Context, input *UpdateEnvironmentInput) (*handlerutil.Out[environment.Environment], error) {
	req := &input.Body
	updates := map[string]any{}
	if input.ID != LocalEnvironmentID {
		if req.ApiUrl != nil {
			updates["api_url"] = *req.ApiUrl
		}
		if req.Enabled != nil {
			updates["enabled"] = *req.Enabled
		}
		if req.AccessToken != nil {
			updates["access_token"] = *req.AccessToken
		}
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}

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
			status, err := h.environmentService.TestConnection(syncCtx, envID, nil)
			if err != nil {
				slog.WarnContext(syncCtx, "Failed to test connection after environment update",
					"environmentId", envID, "environmentName", envName, "status", status, "error", err)
			}
		}(context.WithoutCancel(ctx), input.ID, updated.Name)
	}
	if updated.AccessToken != nil && *updated.AccessToken != "" && ((req.AccessToken != nil && *req.AccessToken != "") || req.Name != nil) {
		h.triggerEnvironmentResourceSync(ctx, input.ID, updated.Name, "environment update")
	}

	var newApiKey *string
	if req.RegenerateApiKey != nil && *req.RegenerateApiKey {
		localUser, err := handlerutil.RequireUser(ctx)
		if err != nil {
			return nil, err
		}

		apiKey, err := h.environmentService.RegenerateEnvironmentApiKey(ctx, updated, localUser.ID, localUser.Username)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to regenerate API key", "environmentId", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		updated, err = h.environmentService.GetEnvironmentByID(ctx, input.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to fetch updated environment", "environmentId", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to fetch updated environment")
		}
		newApiKey = new(apiKey)
	}

	out, mapErr := mapping.MapOne[*Environment, environment.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	ApplyEnvironmentRuntimeState(&out)
	out.ApiKey = newApiKey

	return &handlerutil.Out[environment.Environment]{
		Body: base.ApiResponse[environment.Environment]{
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
func (h *EnvironmentHandler) DeleteEnvironment(ctx context.Context, input *DeleteEnvironmentInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ID == LocalEnvironmentID {
		return nil, huma.Error400BadRequest("Cannot delete local environment")
	}

	userID, username := currentUserRefs(ctx)
	if err := h.environmentService.DeleteEnvironment(ctx, input.ID, userID, username); err != nil {
		return nil, huma.Error500InternalServerError("Failed to delete environment: " + err.Error())
	}

	return handlerutil.MessageOutput("Environment deleted successfully", ""), nil
}

// TestConnection tests connectivity to an environment.
func (h *EnvironmentHandler) TestConnection(ctx context.Context, input *TestConnectionInput) (*handlerutil.Out[environment.Test], error) {
	var apiUrl *string
	if input.Body != nil {
		apiUrl = input.Body.ApiUrl
	}
	if apiUrl != nil {
		permissions, ok := middleware.PermissionsFromContext(ctx)
		if !ok || !permissions.Allows(authz.PermEnvironmentsUpdate, "") {
			return nil, huma.Error403Forbidden("permission denied: " + authz.PermEnvironmentsUpdate)
		}
	}

	status, err := h.environmentService.TestConnection(ctx, input.ID, apiUrl)
	resp := environment.Test{Status: status}
	if err != nil {
		if apiUrl == nil {
			resp.Message = new(err.Error())
		} else {
			apiErr := common.ToAPIError(err)
			err = huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return &handlerutil.Out[environment.Test]{
			Body: base.ApiResponse[environment.Test]{
				Success: false,
				Data:    resp,
			},
		}, err
	}

	return &handlerutil.Out[environment.Test]{
		Body: base.ApiResponse[environment.Test]{
			Success: true,
			Data:    resp,
		},
	}, nil
}

// UpdateHeartbeat updates the heartbeat for an environment.
func (h *EnvironmentHandler) UpdateHeartbeat(ctx context.Context, input *UpdateHeartbeatInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError("Failed to update heartbeat")
	}

	return handlerutil.MessageOutput("Heartbeat updated successfully", ""), nil
}

// PairAgent generates or rotates the local agent pairing token.
func (h *EnvironmentHandler) PairAgent(ctx context.Context, input *PairAgentInput) (*handlerutil.Out[environment.AgentPairResponse], error) {
	if input.ID != LocalEnvironmentID {
		return nil, huma.Error404NotFound("Not found")
	}

	shouldRotate := input.Body != nil && input.Body.Rotate != nil && *input.Body.Rotate
	if h.cfg.AgentToken == "" || shouldRotate {
		h.cfg.AgentToken = kit.RandomString(48)
	}

	if err := h.settingsService.SetStringSetting(ctx, "agentToken", h.cfg.AgentToken); err != nil {
		return nil, huma.Error500InternalServerError("Failed to persist agent token")
	}

	return &handlerutil.Out[environment.AgentPairResponse]{
		Body: base.ApiResponse[environment.AgentPairResponse]{
			Success: true,
			Data: environment.AgentPairResponse{
				Token: h.cfg.AgentToken,
			},
		},
	}, nil
}

// SyncEnvironment syncs manager-owned resources to an environment.
func (h *EnvironmentHandler) SyncEnvironment(ctx context.Context, input *SyncEnvironmentInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := h.environmentService.SyncResourcesToEnvironment(runtimeCtx, input.ID, user, h.activityService)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
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
func (h *EnvironmentHandler) PairEnvironment(ctx context.Context, input *PairEnvironmentInput) (*handlerutil.Out[base.MessageResponse], error) {
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
func (h *EnvironmentHandler) GetDeploymentSnippets(ctx context.Context, input *GetDeploymentSnippetsInput) (*handlerutil.Out[DeploymentSnippet], error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	if env.ApiKeyID == nil {
		return nil, huma.Error400BadRequest("Environment does not have an API key configured")
	}

	if env.AccessToken == nil || *env.AccessToken == "" {
		return nil, huma.Error400BadRequest("Environment is missing access token")
	}

	var snippets *DeploymentSnippets
	if env.IsEdge {
		snippets, err = h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), *env.AccessToken, h.edgeMTLSConfig())
	} else {
		snippets, err = h.environmentService.GenerateDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), env.ApiUrl, *env.AccessToken)
	}
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate deployment snippets", "environmentId", input.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to generate deployment snippets")
	}

	var mtls *DeploymentSnippetMTLS
	if snippets.MTLS != nil {
		files := make([]DeploymentSnippetFile, 0, len(snippets.MTLS.Files))
		for _, file := range snippets.MTLS.Files {
			entry := DeploymentSnippetFile{
				Name:          file.Name,
				ContainerPath: file.ContainerPath,
				Permissions:   file.Permissions,
				DownloadURL:   fmt.Sprintf("/api/environments/%s/deployment/mtls/%s", env.ID, file.Name),
			}
			// Secret material is only served through the download endpoint.
			if isSensitiveMTLSAssetName(file.Name) {
				entry.Sensitive = true
			} else {
				entry.Content = file.Content
			}
			files = append(files, entry)
		}
		mtls = &DeploymentSnippetMTLS{
			DockerRun:     snippets.MTLS.DockerRun,
			DockerCompose: snippets.MTLS.DockerCompose,
			Files:         files,
			HostDirHint:   snippets.MTLS.HostDirHint,
		}
	}

	return &handlerutil.Out[DeploymentSnippet]{
		Body: base.ApiResponse[DeploymentSnippet]{
			Success: true,
			Data: DeploymentSnippet{
				DockerRun:     snippets.DockerRun,
				DockerCompose: snippets.DockerCompose,
				MTLS:          mtls,
			},
		},
	}, nil
}

// GetEnvironmentVersion returns the version of the local or a remote environment.
func (h *EnvironmentHandler) GetEnvironmentVersion(ctx context.Context, input *GetEnvironmentVersionInput) (*handlerutil.Out[version.Info], error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var versionInfo version.Info
	if env.ID == LocalEnvironmentID && h.localVersion != nil {
		versionInfo = *h.localVersion(reqCtx)
	} else if proxyErr := h.environmentService.ProxyJSONRequestForEnvironment(reqCtx, *env, http.MethodGet, "/api/app-version", nil, &versionInfo); proxyErr != nil {
		return nil, handlerutil.TranslateRemoteProxyError(proxyErr)
	}

	// A successful reply proves the environment is online.
	if updateErr := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); updateErr != nil {
		slog.WarnContext(ctx, "Failed to update environment heartbeat", "environmentId", input.ID, "error", updateErr)
	}

	return &handlerutil.Out[version.Info]{
		Body: base.ApiResponse[version.Info]{
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
		mode := kit.Ternary[os.FileMode](isSensitiveMTLSAssetName(file.Name), 0o600, 0o644)
		if parsed, parseErr := strconv.ParseUint(strings.TrimSpace(file.Permissions), 8, 32); parseErr == nil && parsed != 0 {
			mode = os.FileMode(parsed)
		}
		header.SetMode(mode)

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
	index := slices.IndexFunc(files, func(file DeploymentSnippetFile) bool { return file.Name == input.FileName })
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
	if h.cfg == nil {
		return nil
	}
	return &edge.Config{
		EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
		EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
		AppURL:            h.cfg.GetAppURL(),
	}
}

func (h *EnvironmentHandler) loadEnvironmentMTLSFiles(ctx context.Context, environmentID string) (*Environment, []DeploymentSnippetFile, error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return nil, nil, huma.Error404NotFound("Environment not found")
	}
	if !env.IsEdge {
		return nil, nil, huma.Error400BadRequest("Environment is not an edge agent")
	}
	if env.ApiKeyID == nil {
		return nil, nil, huma.Error400BadRequest("Environment does not have an API key configured")
	}
	if env.AccessToken == nil || *env.AccessToken == "" {
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
			if h.eventService == nil {
				return
			}

			if remoteAddr := strings.TrimSpace(middleware.GetRemoteAddrFromContext(ctx)); remoteAddr != "" {
				metadata["remoteAddr"] = remoteAddr
			}
			userID, username := currentUserRefs(ctx)
			req := event.CreateEventRequest{
				Type:        event.EventTypeEnvironmentMTLSDownload,
				Severity:    event.EventSeverityInfo,
				Title:       title,
				Description: description,
				UserID:      userID,
				Username:    username,
				Metadata:    metadata,
			}
			if env != nil {
				req.ResourceType = new("environment")
				req.ResourceID = new(env.ID)
				req.ResourceName = new(env.Name)
				req.EnvironmentID = new(env.ID)
			}
			if _, err := h.eventService.CreateEvent(ctx, req); err != nil {
				slog.WarnContext(ctx, "Failed to record mTLS audit event", "type", string(req.Type), "error", err)
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

func readGeneratedEdgeMTLSCertificateInfo(edgeCfg *edge.Config, envID string) (*environment.EdgeMTLSCertificate, error) {
	if edgeCfg == nil {
		return nil, errors.New("config not available")
	}
	if edge.NormalizeEdgeMTLSMode(edgeCfg.EdgeMTLSMode) == edge.EdgeMTLSModeDisabled {
		return nil, errors.New("edge mTLS is disabled")
	}

	certPath, err := edge.GeneratedManagerClientMTLSCertPath(edgeCfg, envID)
	if err != nil {
		return nil, fmt.Errorf("resolve generated edge mTLS client certificate path: %w", err)
	}
	// os rather than acfs: the assets dir may be configured anywhere on the host.
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read generated edge mTLS client certificate: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("decode generated edge mTLS client certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse generated edge mTLS client certificate: %w", err)
	}

	expiresAt := cert.NotAfter.UTC()
	now := time.Now().UTC()
	remaining := expiresAt.Sub(now)
	info := &environment.EdgeMTLSCertificate{
		ExpiresAt:     &expiresAt,
		DaysRemaining: new(int(math.Ceil(max(remaining, 0).Hours() / 24))),
		Expired:       now.After(expiresAt),
		ExpiringSoon:  now.Before(expiresAt) && remaining <= edgeMTLSCertificateExpiryWarningWindow,
	}

	if commonName := strings.TrimSpace(cert.Subject.CommonName); commonName != "" {
		info.CommonName = &commonName
	}

	return info, nil
}
