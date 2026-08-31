package environment

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"archive/zip"
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/getarcaneapp/arcane/types/v2/version"
	"go.getarcane.app/streams/agg"
)

const localDockerEnvironmentID = "0"

const (
	// Only covers poll-mode TTL expiry; tunnel and health-check changes arrive
	// on the service's runtime-change signal instead.
	environmentStreamPollInterval = 5 * time.Second
	environmentStreamRefreshFloor = 30 * time.Second
)

// EnvironmentHandler handles environment management endpoints.
type EnvironmentHandler struct {
	environmentService *EnvironmentService
	settingsService    *settings.SettingsService
	apiKeyService      *apikey.ApiKeyService
	eventService       *event.EventService
	cfg                *config.Config
}

// ============================================================================
// Input/Output Types
// ============================================================================

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

// ============================================================================
// Registration
// ============================================================================

// NewHandler builds the environment HTTP handler and its stream producer.
func NewHandler(environmentService *EnvironmentService, settingsService *settings.SettingsService, apiKeyService *apikey.ApiKeyService, eventService *event.EventService, cfg *config.Config) *EnvironmentHandler {
	return &EnvironmentHandler{
		environmentService: environmentService,
		settingsService:    settingsService,
		apiKeyService:      apiKeyService,
		eventService:       eventService,
		cfg:                cfg,
	}
}

// RegisterEnvironments registers all environment management endpoints.
func RegisterEnvironments(api huma.API, h *EnvironmentHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listEnvironments",
		Method:      "GET",
		Path:        "/environments",
		Summary:     "List environments",
		Description: "Get a paginated list of Docker environments",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		// No global PermEnvironmentsList gate: this endpoint also backs the
		// environment switcher, so any authenticated caller may list. The handler
		// filters the result to the environments the caller can actually access.
		// Management mutations (create/update/delete) remain global-gated below.
	}, h.ListEnvironments)

	huma.Register(api, huma.Operation{
		OperationID: "createEnvironment",
		Method:      "POST",
		Path:        "/environments",
		Summary:     "Create an environment",
		Description: "Create a new Docker environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsCreate),
	}, h.CreateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "getEnvironment",
		Method:      "GET",
		Path:        "/environments/{id}",
		Summary:     "Get an environment",
		Description: "Get a Docker environment by ID",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsRead),
	}, h.GetEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "updateEnvironment",
		Method:      "PUT",
		Path:        "/environments/{id}",
		Summary:     "Update an environment",
		Description: "Update a Docker environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsUpdate),
	}, h.UpdateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEnvironment",
		Method:      "DELETE",
		Path:        "/environments/{id}",
		Summary:     "Delete an environment",
		Description: "Delete a Arcane environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsDelete),
	}, h.DeleteEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "testConnection",
		Method:      "POST",
		Path:        "/environments/{id}/test",
		Summary:     "Test environment connection",
		Description: "Test connectivity to a Arcane environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRead, h.TestConnection)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateHeartbeat",
		Method:      "POST",
		Path:        "/environments/{id}/heartbeat",
		Summary:     "Update environment heartbeat",
		Description: "Update the heartbeat timestamp for an environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsSync, h.UpdateHeartbeat)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pairAgent",
		Method:      "POST",
		Path:        "/environments/{id}/agent/pair",
		Summary:     "Pair with local agent",
		Description: "Generate or rotate the local agent pairing token",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.PairAgent)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncEnvironment",
		Method:      "POST",
		Path:        "/environments/{id}/sync",
		Summary:     "Sync environment",
		Description: "Sync container registries and git repositories to a remote environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsSync, h.SyncEnvironment)

	huma.Register(api, huma.Operation{
		OperationID:  "pairEnvironment",
		Method:       "POST",
		Path:         "/environments/pair",
		Summary:      "Pair agent with manager",
		Description:  "Agent sends API key to complete environment pairing",
		Tags:         []string{"Environments"},
		MaxBodyBytes: 1024,
		Security:     []map[string][]string{},
	}, h.PairEnvironment)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getDeploymentSnippets",
		Method:      "GET",
		Path:        "/environments/{id}/deployment",
		Summary:     "Get deployment snippets",
		Description: "Get Docker run and compose snippets for environment deployment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.GetDeploymentSnippets)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSBundle",
		Method:      "GET",
		Path:        "/environments/{id}/deployment/mtls/bundle",
		Summary:     "Download environment mTLS bundle",
		Description: "Download the generated mTLS client certificate bundle for an edge environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSBundle)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSFile",
		Method:      "GET",
		Path:        "/environments/{id}/deployment/mtls/{fileName}",
		Summary:     "Download environment mTLS asset",
		Description: "Download an individual generated mTLS client certificate asset for an edge environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEnvironmentVersion",
		Method:      "GET",
		Path:        "/environments/{id}/version",
		Summary:     "Get environment version",
		Description: "Get the version of a remote environment",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermEnvironmentsRead, h.GetEnvironmentVersion)

	huma.Register(api, huma.Operation{
		OperationID: "downloadEdgeMTLSCA",
		Method:      "GET",
		Path:        "/edge-mtls/ca",
		Summary:     "Download Arcane-generated edge mTLS CA",
		Description: "Download the Arcane-managed certificate authority used for generated edge mTLS client certificates",
		Tags:        []string{"Environments"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermEnvironmentsPair),
	}, h.DownloadEdgeMTLSCA)
}

// ============================================================================
// Handler Methods
// ============================================================================

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
	ids := make([]string, 0, len(ps.PerEnv))
	for envID := range ps.PerEnv {
		ids = append(ids, envID)
	}
	sort.Strings(ids)
	return ids
}

// visibleEnvironmentsForInternal returns the environments the caller may see,
// with the manager's runtime overlay already applied. It reuses the same access
// rules as ListEnvironments so the stream and the REST list can never disagree
// about which environments a caller has.
func (h *EnvironmentHandler) visibleEnvironmentsForInternal(ctx context.Context, ps *authz.PermissionSet) ([]environment.Environment, error) {
	envs, err := h.environmentService.ListVisibleEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	if environmentListerSeesAllInternal(ps) {
		return envs, nil
	}

	allowed := make(map[string]struct{}, len(ps.PerEnv))
	for _, envID := range accessibleEnvironmentIDsInternal(ps) {
		allowed[envID] = struct{}{}
	}

	filtered := envs[:0]
	for _, env := range envs {
		if _, ok := allowed[env.ID]; ok {
			filtered = append(filtered, env)
		}
	}
	return filtered, nil
}

// fingerprintEnvironmentsInternal hashes every field of the visible environment
// list. This runs on every stream tick for every connected client, so it hashes
// the fields directly rather than marshalling the payload to JSON and retaining
// the bytes for comparison — most ticks change nothing and the encoded snapshot
// was thrown away immediately.
func fingerprintEnvironmentsInternal(envs []environment.Environment) uint64 {
	return utils.NewFingerprint().Slice(envs, func(f *utils.Fingerprint, env *environment.Environment) {
		f.String(env.ID).
			String(env.Name).
			String(env.ApiUrl).
			String(env.Status).
			Bool(env.Enabled).
			Bool(env.IsEdge).
			OptTime(env.LastSeen).
			OptString(env.EdgeTransport).
			OptString(env.LastEdgeTransport).
			OptString(env.EdgeSecurityMode).
			OptString(env.EdgeSessionID).
			OptString(env.EdgeAgentInstance).
			Strings(env.EdgeCapabilities).
			OptBool(env.Connected).
			OptTime(env.ConnectedAt).
			OptTime(env.LastHeartbeat).
			OptTime(env.LastPollAt).
			OptString(env.ApiKey)

		cert := env.EdgeMTLSCertificate
		f.Present(cert != nil)
		if cert == nil {
			return
		}
		f.OptString(cert.CommonName).
			OptTime(cert.ExpiresAt).
			OptInt(cert.DaysRemaining).
			Bool(cert.Expired).
			Bool(cert.ExpiringSoon)
	}).Sum()
}

func (h *EnvironmentHandler) RunStreamProducer(ctx context.Context, ps *authz.PermissionSet, events chan<- environment.StreamEvent) {
	changes, unsubscribe := h.environmentService.SubscribeRuntimeChanges()
	defer unsubscribe()

	var lastFingerprint uint64
	var haveFingerprint bool
	var lastSentAt time.Time

	send := func() bool {
		envs, err := h.visibleEnvironmentsForInternal(ctx, ps)
		if err != nil {
			// A failed read must not end the stream; the next tick retries.
			if ctx.Err() == nil {
				slog.WarnContext(ctx, "environment stream failed to list environments", "error", err)
			}
			return ctx.Err() == nil
		}

		fingerprint := fingerprintEnvironmentsInternal(envs)
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
	}

	if !send() {
		return
	}

	ticker := time.NewTicker(environmentStreamPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			if !send() {
				return
			}
		case <-ticker.C:
			if !send() {
				return
			}
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

func (h *EnvironmentHandler) createEnvironmentWithApiKeyInternal(ctx context.Context, env *Environment, user *common.User) (*handlerutil.Out[EnvironmentWithApiKey], error) {
	// New API key-based pairing flow
	env.Status = string(EnvironmentStatusPending)

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to create environment").Error())
	}

	// Generate API key for environment
	apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, created.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create environment API key", "environmentID", created.ID, "error", err.Error())
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
			slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentID", created.ID, "error", delErr.Error())
		}
		slog.ErrorContext(ctx, "Failed to link API key to environment", "environmentID", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to link API key")
	}
	created = updated

	out, mapErr := mapper.MapOne[*Environment, environment.Environment](created)
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

func (h *EnvironmentHandler) createEnvironmentLegacyInternal(ctx context.Context, env *Environment, user *common.User, body environment.Create) (*handlerutil.Out[EnvironmentWithApiKey], error) {
	if body.AccessToken != nil && *body.AccessToken != "" {
		env.AccessToken = body.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to create environment").Error())
	}

	// Sync registries and git repositories in background (intentionally detached from request context)
	if created.AccessToken != nil && *created.AccessToken != "" {
		h.triggerEnvironmentResourceSyncInternal(ctx, created.ID, created.Name, "environment creation")
	}

	out, mapErr := mapper.MapOne[*Environment, environment.Environment](created)
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

	out, mapErr := mapper.MapOne[*Environment, environment.Environment](env)
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

	user, _ := common.CurrentUserFromContext(ctx)
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

	out, mapErr := mapper.MapOne[*Environment, environment.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map environment")
	}
	h.applyEdgeRuntimeStateInternal(&out)

	// If regenerating API key, return the new key
	var newApiKey *string
	if input.Body.RegenerateApiKey != nil && *input.Body.RegenerateApiKey {
		user, err := handlerutil.RequireUser(ctx)
		if err != nil {
			return nil, err
		}

		oldApiKeyID := updated.ApiKeyID

		// Generate new API key
		apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, input.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create new environment API key", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Use service method to update environment and create event
		apiKey := apiKeyDto.Key
		err = h.environmentService.RegenerateEnvironmentApiKey(ctx, input.ID, apiKeyDto.ID, apiKey, user.ID, user.Username, updated.Name)
		if err != nil {
			// The new key was never linked; remove it so a failed rotation does
			// not leave an orphaned valid credential behind.
			if delErr := h.apiKeyService.DeleteApiKey(ctx, apiKeyDto.ID); delErr != nil && !errors.Is(delErr, apikey.ErrApiKeyNotFound) {
				slog.ErrorContext(ctx, "Failed to clean up unlinked environment API key", "environmentID", input.ID, "error", delErr.Error())
			}
			slog.ErrorContext(ctx, "Failed to regenerate API key", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Delete the previous key only after the environment points at the new
		// one — while still referenced it is protected and the delete would be
		// rejected, which is how stale bootstrap keys used to accumulate. A
		// failed delete leaves the old key as a still-valid credential, so log
		// it as an error; the key stays visible and deletable on the API Keys
		// page.
		if oldApiKeyID != nil && *oldApiKeyID != apiKeyDto.ID {
			if err := h.apiKeyService.DeleteApiKey(ctx, *oldApiKeyID); err != nil && !errors.Is(err, apikey.ErrApiKeyNotFound) {
				slog.ErrorContext(ctx, "Failed to delete previous environment API key; the old key remains valid until deleted manually", "environmentID", input.ID, "error", err.Error())
			}
		}

		// Fetch updated environment
		updated, err = h.environmentService.GetEnvironmentByID(ctx, input.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to fetch updated environment", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to fetch updated environment")
		}

		// Re-map with updated environment data
		out, mapErr = mapper.MapOne[*Environment, environment.Environment](updated)
		if mapErr != nil {
			return nil, huma.Error500InternalServerError("Failed to map environment")
		}
		h.applyEdgeRuntimeStateInternal(&out)

		newApiKey = new(apiKeyDto.Key)
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

	user, _ := common.CurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}
	if err := h.environmentService.DeleteEnvironment(ctx, input.ID, userID, username); err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to delete environment").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment deleted successfully",
			},
		},
	}, nil
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

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Heartbeat updated successfully",
			},
		},
	}, nil
}

// PairAgent generates or rotates the local agent pairing token.
func (h *EnvironmentHandler) PairAgent(ctx context.Context, input *PairAgentInput) (*handlerutil.Out[environment.AgentPairResponse], error) {
	if input.ID != localDockerEnvironmentID {
		return nil, huma.Error404NotFound("Not found")
	}

	shouldRotate := input.Body != nil && input.Body.Rotate != nil && *input.Body.Rotate
	if h.cfg.AgentToken == "" || shouldRotate {
		h.cfg.AgentToken = utils.GenerateRandomString(48)
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
	// Sync registries
	if err := h.environmentService.SyncRegistriesToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync registries", "environmentID", input.ID, "error", err.Error())
	}

	if err := h.environmentService.SyncS3DestinationsToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync S3 destinations", "environmentID", input.ID, "error", err.Error())
	}

	// Sync git repositories
	if err := h.environmentService.SyncRepositoriesToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync git repositories", "environmentID", input.ID, "error", err.Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment synced successfully",
			},
		},
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

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
		go func(syncCtx context.Context, envID string, envName string) {
			status, err := h.environmentService.TestConnection(syncCtx, envID, nil)
			if err != nil {
				slog.WarnContext(syncCtx, "Failed to test connection after environment update",
					"environment_id", envID, "environment_name", envName, "status", status, "error", err)
			}
		}(detachedCtx, environmentID, updated.Name)
	}

	if updated.AccessToken != nil && *updated.AccessToken != "" && ((req.AccessToken != nil && *req.AccessToken != "") || req.Name != nil) {
		h.triggerEnvironmentResourceSyncInternal(ctx, environmentID, updated.Name, "environment update")
	}
}

func (h *EnvironmentHandler) triggerEnvironmentResourceSyncInternal(ctx context.Context, environmentID string, environmentName string, reason string) {
	detachedCtx := context.WithoutCancel(ctx)

	go func(syncCtx context.Context, envID string, envName string, syncReason string) {
		syncCtx, cancel := context.WithTimeout(syncCtx, edge.DefaultProxyTimeout)
		defer cancel()
		if err := h.environmentService.SyncRegistriesToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync registries to environment",
				"environmentID", envID,
				"environmentName", envName,
				"reason", syncReason,
				"error", err.Error())
		}
	}(detachedCtx, environmentID, environmentName, reason)

	go func(syncCtx context.Context, envID string, envName string, syncReason string) {
		syncCtx, cancel := context.WithTimeout(syncCtx, edge.DefaultProxyTimeout)
		defer cancel()
		if err := h.environmentService.SyncS3DestinationsToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync S3 destinations to environment",
				"environmentID", envID,
				"environmentName", envName,
				"reason", syncReason,
				"error", err.Error())
		}
	}(detachedCtx, environmentID, environmentName, reason)

	go func(syncCtx context.Context, envID string, envName string, syncReason string) {
		syncCtx, cancel := context.WithTimeout(syncCtx, edge.DefaultProxyTimeout)
		defer cancel()
		if err := h.environmentService.SyncRepositoriesToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync git repositories to environment",
				"environmentID", envID,
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
		slog.ErrorContext(ctx, "Failed to get environment", "environmentID", *envID, "error", err.Error())
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
		slog.ErrorContext(ctx, "Failed to update environment status", "environmentID", *envID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to complete pairing")
	}

	slog.InfoContext(ctx, "Environment pairing completed", "environmentID", *envID, "environmentName", env.Name)
	h.triggerEnvironmentResourceSyncInternal(ctx, *envID, env.Name, "environment pairing")

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment pairing completed successfully",
			},
		},
	}, nil
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
		snippets, err = h.environmentService.GenerateDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), *env.AccessToken)
	}
	if err != nil {
		slog.ErrorContext(ctx, "Failed to generate deployment snippets", "environmentID", input.ID, "error", err.Error())
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

		statusCode, respBody, err := edge.DoRequest(reqCtx, input.ID, http.MethodGet, "/api/app-version", nil)
		if err != nil {
			return nil, huma.Error500InternalServerError("Request via tunnel failed: " + err.Error())
		}
		if statusCode != http.StatusOK {
			return nil, huma.Error500InternalServerError(fmt.Sprintf("Unexpected status code: %d", statusCode))
		}

		if err := json.Unmarshal(respBody, &versionInfo); err != nil {
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

		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, validatedURL.String(), nil)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to create request")
		}

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, huma.Error500InternalServerError("Request failed: " + err.Error())
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			return nil, huma.Error500InternalServerError(fmt.Sprintf("Unexpected status code: %d", resp.StatusCode))
		}

		if err := json.UnmarshalRead(resp.Body, &versionInfo); err != nil {
			return nil, huma.Error500InternalServerError("Failed to decode version response")
		}
	}

	// Update environment status to online since we successfully contacted it
	if updateErr := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); updateErr != nil {
		slog.WarnContext(ctx, "Failed to update environment heartbeat", "environment_id", input.ID, "error", updateErr)
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
			slog.ErrorContext(ctx, "Failed to create mTLS bundle entry", "environmentID", input.ID, "fileName", downloadName, "error", createErr.Error())
			return nil, huma.Error500InternalServerError("Failed to build mTLS bundle")
		}

		if _, writeErr := entry.Write([]byte(file.Content)); writeErr != nil {
			slog.ErrorContext(ctx, "Failed to write mTLS bundle entry", "environmentID", input.ID, "fileName", downloadName, "error", writeErr.Error())
			return nil, huma.Error500InternalServerError("Failed to build mTLS bundle")
		}
	}

	if err := zipWriter.Close(); err != nil {
		slog.ErrorContext(ctx, "Failed to finalize mTLS bundle", "environmentID", input.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to build mTLS bundle")
	}

	fileName := environmentMTLSDownloadBaseNameInternal(env) + "-mtls.zip"

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/zip")
			humaCtx.SetHeader("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
			humaCtx.SetHeader("Content-Length", strconv.Itoa(archive.Len()))

			if written, writeErr := humaCtx.BodyWriter().Write(archive.Bytes()); writeErr != nil || written != archive.Len() {
				slog.WarnContext(humaCtx.Context(), "Failed to stream edge mTLS bundle download", "environmentID", input.ID, "fileName", fileName, "bytesWritten", written, "bytesExpected", archive.Len(), "error", writeErr)
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
				slog.WarnContext(humaCtx.Context(), "Failed to stream edge mTLS asset download", "environmentID", input.ID, "fileName", file.Name, "bytesWritten", written, "bytesExpected", len(fileContent), "error", writeErr)
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
		slog.ErrorContext(ctx, "Failed to generate environment mTLS assets", "environmentID", environmentID, "error", err.Error())
		return nil, nil, huma.Error500InternalServerError("Failed to generate environment mTLS assets")
	}

	if snippets.MTLS == nil || len(snippets.MTLS.Files) == 0 {
		return nil, nil, huma.Error404NotFound("mTLS assets are not available for this environment")
	}

	return env, snippets.MTLS.Files, nil
}

func (h *EnvironmentHandler) loadEnvironmentMTLSFileInternal(ctx context.Context, environmentID string, fileName string) (*Environment, DeploymentSnippetFile, error) {
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
	baseName := strings.TrimSpace(env.Name)
	if baseName == "" {
		baseName = "environment"
	}

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

	baseName = strings.Trim(baseName, "-")
	if baseName == "" {
		baseName = "environment"
	}

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
	if isSensitiveMTLSAssetNameInternal(file.Name) {
		return 0o600
	}
	return 0o644
}

// logMTLSAuditEventInternal records an audit event for administrator-triggered
// edge mTLS actions (downloads, bundle exports). Must never include raw
// certificate content or private key material.
func (h *EnvironmentHandler) logMTLSAuditEventInternal(ctx context.Context, env *Environment, eventType event.EventType, title, description string, extra database.JSON) {
	if h == nil || h.eventService == nil {
		return
	}

	user, _ := common.CurrentUserFromContext(ctx)
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
