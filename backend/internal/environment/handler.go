package environment

import (
	"archive/zip"
	"bytes"
	"cmp"
	"context"
	"crypto/x509"
	"encoding/json/v2"
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
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

const (
	localDockerEnvironmentID = "0"

	// Re-applies in-memory runtime state to cached rows, covering poll-mode TTL
	// expiry; CRUD, tunnel and health-check changes arrive on the runtime-change signal.
	environmentStreamRuntimeInterval = 5 * time.Second
	// Safety net for writes that bypass the runtime-change signal.
	environmentStreamReloadInterval = 60 * time.Second
	environmentStreamRefreshFloor   = 30 * time.Second
	environmentStreamHubKeyInternal = "environments"
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
	// The list endpoint backs both the environments management page and the
	// environment switcher, so any authenticated caller may reach it. Global
	// listers (sudo, global admins, or holders of the org-level
	// environments:list permission) see every environment; environment-scoped
	// callers see only the environments they hold at least one permission on.
	ps, ok := middleware.PermissionsFromContext(ctx)
	if !ok {
		return nil, huma.Error403Forbidden("permission denied")
	}
	var accessibleEnvIDs []string // nil = no restriction
	if !environmentListerSeesAllInternal(ps) {
		accessibleEnvIDs = accessibleEnvironmentIDsInternal(ps)
	}

	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	envs, paginationResp, err := h.environmentService.ListEnvironmentsPaginated(ctx, params, accessibleEnvIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch environments")
	}
	for i := range envs {
		h.applyEdgeRuntimeStateInternal(&envs[i])
	}

	return &handlerutil.Page[environment.Environment]{
		Body: base.Paginated[environment.Environment]{
			Success:    true,
			Data:       envs,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// environmentListerSeesAllInternal reports whether the caller may list every
// environment. True for sudo callers, global admins, and holders of the
// org-level environments:list permission (Allows short-circuits on sudo and
// treats global admins as holding every permission).
func environmentListerSeesAllInternal(ps *authz.PermissionSet) bool {
	return ps != nil && ps.Allows(authz.PermEnvironmentsList, "")
}

// accessibleEnvironmentIDsInternal returns the sorted set of environment IDs the
// caller holds at least one environment-scoped permission on. A non-nil result
// (possibly empty) restricts the environment list for non-global callers; an
// empty result yields no environments.
func accessibleEnvironmentIDsInternal(ps *authz.PermissionSet) []string {
	if ps == nil {
		return []string{}
	}
	ids := slices.Sorted(maps.Keys(ps.PerEnv))
	return ids
}

// visibleEnvironmentsForInternal filters the shared environment snapshot down
// to what the caller may see. It reuses the same access rules as
// ListEnvironments so the stream and the REST list can never disagree about
// which environments a caller has. The input slice is shared across stream
// subscribers and must not be mutated.
func visibleEnvironmentsForInternal(envs []environment.Environment, ps *authz.PermissionSet) []environment.Environment {
	if environmentListerSeesAllInternal(ps) {
		return envs
	}

	allowed := make(map[string]struct{}, len(ps.PerEnv))
	for _, envID := range accessibleEnvironmentIDsInternal(ps) {
		allowed[envID] = struct{}{}
	}

	filtered := make([]environment.Environment, 0, len(allowed))
	for _, env := range envs {
		if _, ok := allowed[env.ID]; ok {
			filtered = append(filtered, env)
		}
	}
	return filtered
}

func (h *EnvironmentHandler) RunStreamProducer(ctx context.Context, ps *authz.PermissionSet, events chan<- environment.StreamEvent) {
	var lastFingerprint uint64
	var haveFingerprint bool
	var lastSentAt time.Time

	h.streamHub.Subscribe(ctx, environmentStreamHubKeyInternal, h.runEnvironmentStreamListerInternal, func(all []environment.Environment) bool {
		envs := visibleEnvironmentsForInternal(all, ps)
		fingerprint := kit.Fingerprint(envs)
		// Re-send unchanged state on a floor so relative timestamps in the UI
		// ("last seen 2 minutes ago") keep advancing.
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

func (h *EnvironmentHandler) runEnvironmentStreamListerInternal(ctx context.Context, publish func([]environment.Environment)) {
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

	// Determine pairing method
	useApiKey := input.Body.UseApiKey != nil && *input.Body.UseApiKey

	if useApiKey {
		return h.createEnvironmentWithApiKeyInternal(ctx, env, user)
	}

	return h.createEnvironmentLegacyInternal(ctx, env, user, input.Body)
}

func (h *EnvironmentHandler) createEnvironmentWithApiKeyInternal(ctx context.Context, env *Environment, user *usertypes.Actor) (*handlerutil.Out[EnvironmentWithApiKey], error) {
	// New API key-based pairing flow
	env.Status = string(EnvironmentStatusPending)

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create environment: " + err.Error())
	}

	// Generate API key for environment
	apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, created.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create environment API key", "environmentId", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to create environment API key")
	}

	// Store the full API key in AccessToken for manager-to-agent auth.
	apiKey := apiKeyDto.Key

	// Link the API key to the environment for manager use.
	updates := map[string]any{
		"api_key_id":   apiKeyDto.ID,
		"access_token": apiKey,
	}
	updated, err := h.environmentService.UpdateEnvironment(ctx, created.ID, updates, &user.ID, &user.Username)
	if err != nil {
		// Remove the key so a failed create does not leave an orphaned row
		// behind. ErrApiKeyProtected means the link actually landed and the
		// update failed afterwards, so the key legitimately belongs to the
		// environment and must survive; deleting the environment cascades it.
		if delErr := h.apiKeyService.DeleteApiKey(ctx, apiKeyDto.ID); delErr != nil &&
			!errors.Is(delErr, apikey.ErrApiKeyNotFound) && !errors.Is(delErr, apikey.ErrApiKeyProtected) {
			slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentId", created.ID, "error", delErr.Error())
		}
		slog.ErrorContext(ctx, "Failed to link API key to environment", "environmentId", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to link API key")
	}
	created = updated

	out, mapErr := mapping.MapOne[*Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	h.applyEdgeRuntimeStateInternal(&out)

	return &handlerutil.Out[EnvironmentWithApiKey]{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data: EnvironmentWithApiKey{
				Environment: out,
				ApiKey:      new(apiKeyDto.Key),
			},
		},
	}, nil
}

func (h *EnvironmentHandler) createEnvironmentLegacyInternal(ctx context.Context, env *Environment, user *usertypes.Actor, body environment.Create) (*handlerutil.Out[EnvironmentWithApiKey], error) {
	if body.AccessToken != nil && *body.AccessToken != "" {
		env.AccessToken = body.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to create environment: " + err.Error())
	}

	// Sync registries and git repositories in background (intentionally detached from request context)
	if created.AccessToken != nil && *created.AccessToken != "" {
		h.triggerEnvironmentResourceSyncInternal(ctx, created.ID, created.Name, "environment creation")
	}

	out, mapErr := mapping.MapOne[*Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	h.applyEdgeRuntimeStateInternal(&out)

	return &handlerutil.Out[EnvironmentWithApiKey]{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data: EnvironmentWithApiKey{
				Environment: out,
			},
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
	h.applyEdgeRuntimeStateInternal(&out)
	if env.IsEdge {
		if certInfo, certErr := readGeneratedEdgeMTLSCertificateInfoInternal(h.cfg, env.ID); certErr == nil {
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
	isLocalEnv := input.ID == localDockerEnvironmentID
	updates := h.buildUpdateMapInternal(&input.Body, isLocalEnv)

	h.handleEnvironmentPairingInternal(ctx, input.ID, &input.Body, updates, isLocalEnv)

	user, _ := userctx.CurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}
	updated, updateErr := h.environmentService.UpdateEnvironment(ctx, input.ID, updates, userID, username)
	if updateErr != nil {
		apiErr := common.ToAPIError(updateErr)
		if apiErr.HTTPStatus() == http.StatusInternalServerError {
			return nil, huma.Error500InternalServerError("Failed to update environment")
		}
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}

	h.triggerPostUpdateTasksInternal(ctx, input.ID, updated, &input.Body)

	out, mapErr := mapping.MapOne[*Environment, environment.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	h.applyEdgeRuntimeStateInternal(&out)

	// If regenerating API key, return the new key
	var newApiKey *string
	if input.Body.RegenerateApiKey != nil && *input.Body.RegenerateApiKey {
		localUser, err := handlerutil.RequireUser(ctx)
		if err != nil {
			return nil, err
		}

		apiKey, err := h.environmentService.RegenerateEnvironmentApiKey(ctx, updated, localUser.ID, localUser.Username)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to regenerate API key", "environmentId", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Fetch updated environment
		updated, err = h.environmentService.GetEnvironmentByID(ctx, input.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to fetch updated environment", "environmentId", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to fetch updated environment")
		}

		// Re-map with updated environment data
		out, mapErr = mapping.MapOne[*Environment, environment.Environment](updated)
		if mapErr != nil {
			return nil, huma.Error500InternalServerError("Failed to map environment")
		}
		h.applyEdgeRuntimeStateInternal(&out)

		newApiKey = new(apiKey)
	}

	// Set the API key on the response if regenerated
	out.ApiKey = newApiKey

	return &handlerutil.Out[environment.Environment]{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *EnvironmentHandler) applyEdgeRuntimeStateInternal(env *environment.Environment) {
	ApplyEnvironmentRuntimeState(env)
}

// DeleteEnvironment deletes an environment.
func (h *EnvironmentHandler) DeleteEnvironment(ctx context.Context, input *DeleteEnvironmentInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ID == localDockerEnvironmentID {
		return nil, huma.Error400BadRequest("Cannot delete local environment")
	}

	user, _ := userctx.CurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}
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
	if input.ID != localDockerEnvironmentID {
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

func (h *EnvironmentHandler) buildUpdateMapInternal(req *environment.Update, isLocalEnv bool) map[string]any {
	updates := map[string]any{}

	if !isLocalEnv {
		if req.ApiUrl != nil {
			updates["api_url"] = *req.ApiUrl
		}
		if req.Enabled != nil {
			updates["enabled"] = *req.Enabled
		}
	}

	if req.Name != nil {
		updates["name"] = *req.Name
	}

	return updates
}

func (h *EnvironmentHandler) handleEnvironmentPairingInternal(ctx context.Context, environmentID string, req *environment.Update, updates map[string]any, isLocalEnv bool) {
	_ = ctx
	_ = environmentID
	if isLocalEnv {
		return
	}

	if req.AccessToken != nil {
		updates["access_token"] = *req.AccessToken
	}
}

func (h *EnvironmentHandler) triggerPostUpdateTasksInternal(ctx context.Context, environmentID string, updated *Environment, req *environment.Update) {
	if updated.Enabled {
		detachedCtx := context.WithoutCancel(ctx)
		go func(syncCtx context.Context, envID, envName string) {
			status, err := h.environmentService.TestConnection(syncCtx, envID, nil)
			if err != nil {
				slog.WarnContext(syncCtx, "Failed to test connection after environment update",
					"environmentId", envID, "environmentName", envName, "status", status, "error", err)
			}
		}(detachedCtx, environmentID, updated.Name)
	}

	if updated.AccessToken != nil && *updated.AccessToken != "" && ((req.AccessToken != nil && *req.AccessToken != "") || req.Name != nil) {
		h.triggerEnvironmentResourceSyncInternal(ctx, environmentID, updated.Name, "environment update")
	}
}

func (h *EnvironmentHandler) triggerEnvironmentResourceSyncInternal(ctx context.Context, environmentID, environmentName, reason string) {
	h.environmentService.ForgetSyncState(environmentID)
	detachedCtx := context.WithoutCancel(ctx)

	go func(syncCtx context.Context, envID, envName, syncReason string) {
		syncCtx, cancel := context.WithTimeout(syncCtx, edge.DefaultProxyTimeout)
		defer cancel()
		if err := h.environmentService.SyncRegistriesToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync registries to environment",
				"environmentId", envID,
				"environmentName", envName,
				"reason", syncReason,
				"error", err.Error())
		}
	}(detachedCtx, environmentID, environmentName, reason)

	go func(syncCtx context.Context, envID, envName, syncReason string) {
		syncCtx, cancel := context.WithTimeout(syncCtx, edge.DefaultProxyTimeout)
		defer cancel()
		if err := h.environmentService.SyncS3DestinationsToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync S3 destinations to environment",
				"environmentId", envID,
				"environmentName", envName,
				"reason", syncReason,
				"error", err.Error())
		}
	}(detachedCtx, environmentID, environmentName, reason)

	go func(syncCtx context.Context, envID, envName, syncReason string) {
		syncCtx, cancel := context.WithTimeout(syncCtx, edge.DefaultProxyTimeout)
		defer cancel()
		if err := h.environmentService.SyncRepositoriesToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync git repositories to environment",
				"environmentId", envID,
				"environmentName", envName,
				"reason", syncReason,
				"error", err.Error())
		}
	}(detachedCtx, environmentID, environmentName, reason)
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
	h.triggerEnvironmentResourceSyncInternal(ctx, *envID, env.Name, "environment pairing")

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

	// Generate snippets with API key
	// Use edge snippets for edge environments
	var snippets *DeploymentSnippets
	if env.IsEdge {
		snippets, err = h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), *env.AccessToken, &edge.Config{
			EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
			EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
			EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
			AppURL:            h.cfg.GetAppURL(),
		})
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
			sensitive := isSensitiveMTLSAssetNameInternal(file.Name)
			entry := DeploymentSnippetFile{
				Name:          file.Name,
				ContainerPath: file.ContainerPath,
				Permissions:   file.Permissions,
				DownloadURL:   fmt.Sprintf("/api/environments/%s/deployment/mtls/%s", env.ID, file.Name),
			}
			if sensitive {
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

// GetEnvironmentVersion returns the version of a remote environment.
func (h *EnvironmentHandler) GetEnvironmentVersion(ctx context.Context, input *GetEnvironmentVersionInput) (*handlerutil.Out[version.Info], error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var versionInfo version.Info

	// For edge environments, route through the tunnel
	if env.IsEdge {
		if !edge.HasActiveTunnel(input.ID) {
			if _, ok := edge.RequestTunnelAndWait(reqCtx, input.ID, edge.DefaultTunnelDemandTTL, edge.DefaultTunnelAcquireTimeout()).Get(); !ok {
				return nil, huma.Error503ServiceUnavailable("Edge agent is not connected")
			}
		}

		statusCode, respBody, doRequestErr := edge.DoRequest(reqCtx, input.ID, http.MethodGet, "/api/app-version", nil)
		if doRequestErr != nil {
			return nil, huma.Error500InternalServerError("Request via tunnel failed: " + doRequestErr.Error())
		}
		if statusCode != http.StatusOK {
			return nil, huma.Error500InternalServerError(fmt.Sprintf("Unexpected status code: %d", statusCode))
		}

		if unmarshalErr := json.Unmarshal(respBody, &versionInfo); unmarshalErr != nil {
			return nil, huma.Error500InternalServerError("Failed to decode version response")
		}
	} else {
		// Direct HTTP request for non-edge environments
		validatedURL, validateErr := httpx.ValidateOutboundHTTPURL(env.ApiUrl)
		if validateErr != nil {
			return nil, huma.Error400BadRequest("Invalid environment API URL")
		}
		validatedURL.RawQuery = ""
		validatedURL.Fragment = ""
		validatedURL.Path = strings.TrimRight(validatedURL.Path, "/") + "/api/app-version"

		req, newRequestWithContextErr := http.NewRequestWithContext(reqCtx, http.MethodGet, validatedURL.String(), http.NoBody)
		if newRequestWithContextErr != nil {
			return nil, huma.Error500InternalServerError("Failed to create request")
		}

		client := &http.Client{Timeout: 15 * time.Second}
		resp, newRequestWithContextErr := client.Do(req)
		if newRequestWithContextErr != nil {
			return nil, huma.Error500InternalServerError("Request failed: " + newRequestWithContextErr.Error())
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, huma.Error500InternalServerError(fmt.Sprintf("Unexpected status code: %d", resp.StatusCode))
		}

		if unmarshalReadErr := json.UnmarshalRead(resp.Body, &versionInfo); unmarshalReadErr != nil {
			return nil, huma.Error500InternalServerError("Failed to decode version response")
		}
	}

	// Update environment status to online since we successfully contacted it
	if updateErr := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); updateErr != nil {
		slog.WarnContext(ctx, "Failed to update environment heartbeat", "environmentId", input.ID, "error", updateErr)
		// Don't fail the request if heartbeat update fails
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
	var edgeCfg *edge.Config
	if h.cfg != nil {
		edgeCfg = &edge.Config{
			EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
			EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
			EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
		}
	}
	caPath, err := edge.AvailableManagerMTLSCAPath(edgeCfg)
	if err != nil {
		return nil, huma.Error404NotFound("Arcane-managed edge mTLS CA is not available")
	}

	// os.* rather than acfs: the CA path may resolve to a user-configured
	// location anywhere on the host, so no confinement root exists for it.
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to read generated edge mTLS CA", "path", caPath, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to read Arcane-generated edge mTLS CA")
	}

	fileName := filepath.Base(caPath)
	if strings.TrimSpace(fileName) == "" {
		fileName = "ca.crt"
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/x-pem-file")
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			humaCtx.SetHeader("Content-Length", strconv.Itoa(len(caPEM)))

			if written, writeErr := humaCtx.BodyWriter().Write(bytes.Clone(caPEM)); writeErr != nil || written != len(caPEM) {
				slog.WarnContext(humaCtx.Context(), "Failed to stream edge mTLS CA download", "fileName", fileName, "bytesWritten", written, "bytesExpected", len(caPEM), "error", writeErr)
				return
			}
			h.logMTLSAuditEventInternal(humaCtx.Context(), nil, event.EventTypeEnvironmentMTLSDownload,
				"mTLS CA downloaded",
				fmt.Sprintf("Administrator downloaded edge mTLS CA %q", fileName),
				database.JSON{
					"fileName": fileName,
					"kind":     "ca",
				})
		},
	}, nil
}

func (h *EnvironmentHandler) DownloadEnvironmentMTLSBundle(ctx context.Context, input *DownloadEnvironmentMTLSBundleInput) (*huma.StreamResponse, error) {
	env, files, err := h.loadEnvironmentMTLSFilesInternal(ctx, input.ID)
	if err != nil {
		return nil, err
	}

	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)

	for _, file := range files {
		downloadName := environmentMTLSAssetDownloadNameInternal(env, file.Name)
		header := &zip.FileHeader{
			Name:   downloadName,
			Method: zip.Deflate,
		}
		header.SetMode(environmentMTLSAssetFileModeInternal(file))

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

	fileName := environmentMTLSDownloadBaseNameInternal(env) + "-mtls.zip"

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/zip")
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			humaCtx.SetHeader("Content-Length", strconv.Itoa(archive.Len()))

			if written, writeErr := humaCtx.BodyWriter().Write(archive.Bytes()); writeErr != nil || written != archive.Len() {
				slog.WarnContext(
					humaCtx.Context(),
					"Failed to stream edge mTLS bundle download",
					"environmentId",
					input.ID,
					"fileName",
					fileName,
					"bytesWritten",
					written,
					"bytesExpected",
					archive.Len(),
					"error",
					writeErr,
				)
				return
			}
			h.logMTLSAuditEventInternal(humaCtx.Context(), env, event.EventTypeEnvironmentMTLSDownload,
				"mTLS bundle downloaded",
				fmt.Sprintf("Administrator downloaded edge mTLS bundle %q (%d files)", fileName, len(files)),
				database.JSON{
					"fileName":  fileName,
					"kind":      "bundle",
					"fileCount": len(files),
				})
		},
	}, nil
}

func (h *EnvironmentHandler) DownloadEnvironmentMTLSFile(ctx context.Context, input *DownloadEnvironmentMTLSFileInput) (*huma.StreamResponse, error) {
	env, file, err := h.loadEnvironmentMTLSFileInternal(ctx, input.ID, input.FileName)
	if err != nil {
		return nil, err
	}

	fileContent := []byte(file.Content)
	downloadName := environmentMTLSAssetDownloadNameInternal(env, file.Name)

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/x-pem-file")
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
			humaCtx.SetHeader("Content-Length", strconv.Itoa(len(fileContent)))

			if written, writeErr := humaCtx.BodyWriter().Write(fileContent); writeErr != nil || written != len(fileContent) {
				slog.WarnContext(
					humaCtx.Context(),
					"Failed to stream edge mTLS asset download",
					"environmentId",
					input.ID,
					"fileName",
					file.Name,
					"bytesWritten",
					written,
					"bytesExpected",
					len(
						fileContent,
					),
					"error",
					writeErr,
				)
				return
			}
			h.logMTLSAuditEventInternal(humaCtx.Context(), env, event.EventTypeEnvironmentMTLSDownload,
				"mTLS asset downloaded",
				fmt.Sprintf("Administrator downloaded edge mTLS asset %q", file.Name),
				database.JSON{
					"fileName":  file.Name,
					"kind":      "file",
					"sensitive": isSensitiveMTLSAssetNameInternal(file.Name),
				})
		},
	}, nil
}

func (h *EnvironmentHandler) loadEnvironmentMTLSEnvironmentInternal(ctx context.Context, environmentID string) (*Environment, error) {
	env, err := h.environmentService.GetEnvironmentByID(ctx, environmentID)
	if err != nil {
		return nil, huma.Error404NotFound("Environment not found")
	}

	if !env.IsEdge {
		return nil, huma.Error400BadRequest("Environment is not an edge agent")
	}

	if env.ApiKeyID == nil {
		return nil, huma.Error400BadRequest("Environment does not have an API key configured")
	}

	if env.AccessToken == nil || *env.AccessToken == "" {
		return nil, huma.Error400BadRequest("Environment is missing access token")
	}

	return env, nil
}

func (h *EnvironmentHandler) loadEnvironmentMTLSFilesInternal(ctx context.Context, environmentID string) (*Environment, []DeploymentSnippetFile, error) {
	env, err := h.loadEnvironmentMTLSEnvironmentInternal(ctx, environmentID)
	if err != nil {
		return nil, nil, err
	}

	snippets, err := h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), *env.AccessToken, &edge.Config{
		EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
		EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
		AppURL:            h.cfg.GetAppURL(),
	})
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate environment mTLS assets", "environmentId", environmentID, "error", err.Error())
		return nil, nil, huma.Error500InternalServerError("Failed to generate environment mTLS assets")
	}

	if snippets.MTLS == nil || len(snippets.MTLS.Files) == 0 {
		return nil, nil, huma.Error404NotFound("mTLS assets are not available for this environment")
	}

	return env, snippets.MTLS.Files, nil
}

func (h *EnvironmentHandler) loadEnvironmentMTLSFileInternal(ctx context.Context, environmentID, fileName string) (*Environment, DeploymentSnippetFile, error) {
	env, files, err := h.loadEnvironmentMTLSFilesInternal(ctx, environmentID)
	if err != nil {
		return nil, DeploymentSnippetFile{}, err
	}

	for _, file := range files {
		if file.Name == fileName {
			return env, file, nil
		}
	}

	return nil, DeploymentSnippetFile{}, huma.Error404NotFound("Requested mTLS asset was not found")
}

// isSensitiveMTLSAssetNameInternal reports whether the given generated asset
// filename contains secret material (currently just the agent private key).
// Sensitive asset contents must not be returned inline in JSON responses; the
// client should fetch them via the admin-only download endpoint instead.
func isSensitiveMTLSAssetNameInternal(fileName string) bool {
	name := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(name, ".key") || strings.HasSuffix(name, "-key.pem") || strings.HasSuffix(name, "_key.pem")
}

func environmentMTLSDownloadBaseNameInternal(env *Environment) string {
	baseName := cmp.Or(strings.TrimSpace(env.Name), "environment")

	baseName = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '-'
		}
	}, baseName)

	baseName = cmp.Or(strings.Trim(baseName, "-"), "environment")

	return baseName + "-" + env.ID
}

func environmentMTLSAssetDownloadNameInternal(env *Environment, fileName string) string {
	baseName := environmentMTLSDownloadBaseNameInternal(env)

	switch fileName {
	case "agent.crt":
		return baseName + ".pem"
	case "agent.key":
		return baseName + ".key"
	default:
		return fileName
	}
}

func environmentMTLSAssetFileModeInternal(file DeploymentSnippetFile) os.FileMode {
	if parsed, err := strconv.ParseUint(strings.TrimSpace(file.Permissions), 8, 32); err == nil && parsed != 0 {
		return os.FileMode(parsed)
	}
	return kit.Ternary[os.FileMode](isSensitiveMTLSAssetNameInternal(file.Name), 0o600, 0o644)
}

// logMTLSAuditEventInternal records an audit event for administrator-triggered
// edge mTLS actions (downloads, bundle exports). Must never include raw
// certificate content or private key material.
func (h *EnvironmentHandler) logMTLSAuditEventInternal(ctx context.Context, env *Environment, eventType event.EventType, title, description string, extra database.JSON) {
	if h == nil || h.eventService == nil {
		return
	}

	user, _ := userctx.CurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}

	if extra == nil {
		extra = database.JSON{}
	}
	if remoteAddr := strings.TrimSpace(middleware.GetRemoteAddrFromContext(ctx)); remoteAddr != "" {
		extra["remoteAddr"] = remoteAddr
	}

	req := event.CreateEventRequest{
		Type:        eventType,
		Severity:    event.EventSeverityInfo,
		Title:       title,
		Description: description,
		UserID:      userID,
		Username:    username,
		Metadata:    extra,
	}
	if env != nil {
		envID := env.ID
		req.ResourceType = new("environment")
		req.ResourceID = &envID
		req.ResourceName = new(env.Name)
		req.EnvironmentID = &envID
	}

	if _, err := h.eventService.CreateEvent(ctx, req); err != nil {
		slog.WarnContext(ctx, "Failed to record mTLS audit event", "type", string(eventType), "error", err)
	}
}

const edgeMTLSCertificateExpiryWarningWindow = 30 * 24 * time.Hour

func generatedEdgeMTLSClientCertPathInternal(cfg *config.Config, envID string) (string, error) {
	if cfg == nil {
		return "", errors.New("config not available")
	}
	if edge.NormalizeEdgeMTLSMode(cfg.EdgeMTLSMode) == edge.EdgeMTLSModeDisabled {
		return "", errors.New("edge mTLS is disabled")
	}

	edgeCfg := &edge.Config{
		EdgeMTLSAssetsDir: cfg.EdgeMTLSAssetsDir,
	}

	certPath, err := edge.GeneratedManagerClientMTLSCertPath(edgeCfg, envID)
	if err != nil {
		return "", fmt.Errorf("resolve generated edge mTLS client certificate path: %w", err)
	}
	// os.* rather than acfs: the assets dir may be user-configured to anywhere
	// on the host, so no confinement root exists for this path.
	if _, statErr := os.Stat(certPath); statErr != nil {
		return "", fmt.Errorf("stat generated edge mTLS client certificate: %w", statErr)
	}

	return certPath, nil
}

func readGeneratedEdgeMTLSCertificateInfoInternal(cfg *config.Config, envID string) (*environment.EdgeMTLSCertificate, error) {
	certPath, err := generatedEdgeMTLSClientCertPathInternal(cfg, envID)
	if err != nil {
		return nil, err
	}

	// os.* rather than acfs: the assets dir may be user-configured to anywhere
	// on the host, so no confinement root exists for this path.
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
		DaysRemaining: new(edgeMTLSCertificateDaysRemainingInternal(now, expiresAt)),
		Expired:       now.After(expiresAt),
		ExpiringSoon:  now.Before(expiresAt) && remaining <= edgeMTLSCertificateExpiryWarningWindow,
	}

	if commonName := strings.TrimSpace(cert.Subject.CommonName); commonName != "" {
		info.CommonName = &commonName
	}

	return info, nil
}

func edgeMTLSCertificateDaysRemainingInternal(now, expiresAt time.Time) int {
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}
	return int(math.Ceil(remaining.Hours() / 24))
}
