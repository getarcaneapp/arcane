package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/environment"
	"github.com/getarcaneapp/arcane/types/v2/version"
)

const localDockerEnvironmentID = "0"

// EnvironmentHandler handles environment management endpoints.
type EnvironmentHandler struct {
	environmentService *services.EnvironmentService
	settingsService    *services.SettingsService
	apiKeyService      *services.ApiKeyService
	eventService       *services.EventService
	cfg                *config.Config
}

// ============================================================================
// Input/Output Types
// ============================================================================

// EnvironmentPaginatedResponse is the paginated response for environments.
type EnvironmentPaginatedResponse struct {
	Success    bool                      `json:"success"`
	Data       []environment.Environment `json:"data"`
	Pagination base.PaginationResponse   `json:"pagination"`
}

type ListEnvironmentsInput struct {
	Search string `query:"search" doc:"Search query for filtering by name or API URL"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start  int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	Type   string `query:"type" doc:"Filter by environment type (comma-separated: http,edge,websocket,grpc,polling)"`
}

type ListEnvironmentsOutput struct {
	Body EnvironmentPaginatedResponse
}

type CreateEnvironmentInput struct {
	Body environment.Create
}

type EnvironmentWithApiKey struct {
	environment.Environment

	ApiKey *string `json:"apiKey,omitempty" doc:"API key for pairing (only shown once during creation)"`
}

type CreateEnvironmentOutput struct {
	Body base.ApiResponse[EnvironmentWithApiKey]
}

type GetEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentOutput struct {
	Body base.ApiResponse[environment.Environment]
}

type UpdateEnvironmentInput struct {
	ID   string `path:"id" doc:"Environment ID"`
	Body environment.Update
}

type UpdateEnvironmentOutput struct {
	Body base.ApiResponse[environment.Environment]
}

type DeleteEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type DeleteEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type TestConnectionInput struct {
	ID   string                             `path:"id" doc:"Environment ID"`
	Body *environment.TestConnectionRequest `json:"body,omitempty"`
}

type TestConnectionOutput struct {
	Body base.ApiResponse[environment.Test]
}

type UpdateHeartbeatInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type UpdateHeartbeatOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PairAgentInput struct {
	ID   string                        `path:"id" doc:"Environment ID (must be 0 for local)"`
	Body *environment.AgentPairRequest `json:"body,omitempty"`
}

type PairAgentOutput struct {
	Body base.ApiResponse[environment.AgentPairResponse]
}

type SyncEnvironmentInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type SyncEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PairEnvironmentInput struct {
	XAPIKey string `header:"X-API-Key" doc:"API key for environment pairing"`
}

type PairEnvironmentOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DeploymentSnippet struct {
	DockerRun     string                 `json:"dockerRun" doc:"Docker run command snippet"`
	DockerCompose string                 `json:"dockerCompose" doc:"Docker compose YAML snippet"`
	MTLS          *DeploymentSnippetMTLS `json:"mtls,omitempty" doc:"Optional Arcane-generated mTLS deployment assets for edge agents"`
}

type DeploymentSnippetFile struct {
	Name          string `json:"name" doc:"Suggested filename"`
	Content       string `json:"content,omitempty" doc:"PEM file contents. Omitted for sensitive files such as private keys; use downloadUrl instead."`
	DownloadURL   string `json:"downloadUrl,omitempty" doc:"Pairing-permission endpoint to download this file when content is withheld"`
	Sensitive     bool   `json:"sensitive,omitempty" doc:"True when this file is sensitive and must be fetched via downloadUrl"`
	ContainerPath string `json:"containerPath" doc:"Container mount path expected by the mTLS snippet"`
	Permissions   string `json:"permissions" doc:"Suggested file mode"`
}

type DeploymentSnippetMTLS struct {
	DockerRun     string                  `json:"dockerRun" doc:"Docker run snippet using Arcane-generated mTLS assets"`
	DockerCompose string                  `json:"dockerCompose" doc:"Docker compose snippet using Arcane-generated mTLS assets"`
	Files         []DeploymentSnippetFile `json:"files" doc:"Generated PEM files to place on the edge host"`
	HostDirHint   string                  `json:"hostDirHint" doc:"Suggested host directory containing the generated PEM files"`
}

type GetDeploymentSnippetsInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetDeploymentSnippetsOutput struct {
	Body base.ApiResponse[DeploymentSnippet]
}

type GetEnvironmentVersionInput struct {
	ID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentVersionOutput struct {
	Body base.ApiResponse[version.Info]
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

// RegisterEnvironments registers all environment management endpoints.
func RegisterEnvironments(api huma.API, environmentService *services.EnvironmentService, settingsService *services.SettingsService, apiKeyService *services.ApiKeyService, eventService *services.EventService, cfg *config.Config) {
	h := &EnvironmentHandler{
		environmentService: environmentService,
		settingsService:    settingsService,
		apiKeyService:      apiKeyService,
		eventService:       eventService,
		cfg:                cfg,
	}

	huma.Register(api, huma.Operation{
		OperationID: "listEnvironments",
		Method:      "GET",
		Path:        "/environments",
		Summary:     "List environments",
		Description: "Get a paginated list of Docker environments",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
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
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEnvironmentsCreate),
	}, h.CreateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "getEnvironment",
		Method:      "GET",
		Path:        "/environments/{id}",
		Summary:     "Get an environment",
		Description: "Get a Docker environment by ID",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEnvironmentsRead),
	}, h.GetEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "updateEnvironment",
		Method:      "PUT",
		Path:        "/environments/{id}",
		Summary:     "Update an environment",
		Description: "Update a Docker environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEnvironmentsUpdate),
	}, h.UpdateEnvironment)

	huma.Register(api, huma.Operation{
		OperationID: "deleteEnvironment",
		Method:      "DELETE",
		Path:        "/environments/{id}",
		Summary:     "Delete an environment",
		Description: "Delete a Arcane environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEnvironmentsDelete),
	}, h.DeleteEnvironment)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "testConnection",
		Method:      "POST",
		Path:        "/environments/{id}/test",
		Summary:     "Test environment connection",
		Description: "Test connectivity to a Arcane environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsRead, h.TestConnection)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateHeartbeat",
		Method:      "POST",
		Path:        "/environments/{id}/heartbeat",
		Summary:     "Update environment heartbeat",
		Description: "Update the heartbeat timestamp for an environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsSync, h.UpdateHeartbeat)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "pairAgent",
		Method:      "POST",
		Path:        "/environments/{id}/agent/pair",
		Summary:     "Pair with local agent",
		Description: "Generate or rotate the local agent pairing token",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsPair, h.PairAgent)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncEnvironment",
		Method:      "POST",
		Path:        "/environments/{id}/sync",
		Summary:     "Sync environment",
		Description: "Sync container registries and git repositories to a remote environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
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

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "getDeploymentSnippets",
		Method:      "GET",
		Path:        "/environments/{id}/deployment",
		Summary:     "Get deployment snippets",
		Description: "Get Docker run and compose snippets for environment deployment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsPair, h.GetDeploymentSnippets)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSBundle",
		Method:      "GET",
		Path:        "/environments/{id}/deployment/mtls/bundle",
		Summary:     "Download environment mTLS bundle",
		Description: "Download the generated mTLS client certificate bundle for an edge environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSBundle)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "downloadEnvironmentMTLSFile",
		Method:      "GET",
		Path:        "/environments/{id}/deployment/mtls/{fileName}",
		Summary:     "Download environment mTLS asset",
		Description: "Download an individual generated mTLS client certificate asset for an edge environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsPair, h.DownloadEnvironmentMTLSFile)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "getEnvironmentVersion",
		Method:      "GET",
		Path:        "/environments/{id}/version",
		Summary:     "Get environment version",
		Description: "Get the version of a remote environment",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermEnvironmentsRead, h.GetEnvironmentVersion)

	huma.Register(api, huma.Operation{
		OperationID: "downloadEdgeMTLSCA",
		Method:      "GET",
		Path:        "/edge-mtls/ca",
		Summary:     "Download Arcane-generated edge mTLS CA",
		Description: "Download the Arcane-managed certificate authority used for generated edge mTLS client certificates",
		Tags:        []string{"Environments"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermEnvironmentsPair),
	}, h.DownloadEdgeMTLSCA)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListEnvironments returns a paginated list of environments.
func (h *EnvironmentHandler) ListEnvironments(ctx context.Context, input *ListEnvironmentsInput) (*ListEnvironmentsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// The list endpoint backs both the environments management page and the
	// environment switcher, so any authenticated caller may reach it. Global
	// listers (sudo, global admins, or holders of the org-level
	// environments:list permission) see every environment; environment-scoped
	// callers see only the environments they hold at least one permission on.
	ps, ok := humamw.PermissionsFromContext(ctx)
	if !ok {
		return nil, huma.Error403Forbidden("permission denied")
	}
	var accessibleEnvIDs []string // nil = no restriction
	if !environmentListerSeesAllInternal(ps) {
		accessibleEnvIDs = accessibleEnvironmentIDsInternal(ps)
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	envs, paginationResp, err := h.environmentService.ListEnvironmentsPaginated(ctx, params, accessibleEnvIDs)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentListError{Err: err}).Error())
	}
	for i := range envs {
		h.applyEdgeRuntimeStateInternal(&envs[i])
	}

	return &ListEnvironmentsOutput{
		Body: EnvironmentPaginatedResponse{
			Success:    true,
			Data:       envs,
			Pagination: toPaginationResponseInternal(paginationResp),
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

// CreateEnvironment creates a new environment.
func (h *EnvironmentHandler) CreateEnvironment(ctx context.Context, input *CreateEnvironmentInput) (*CreateEnvironmentOutput, error) {
	if h.environmentService == nil || h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	env := &models.Environment{
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

func (h *EnvironmentHandler) createEnvironmentWithApiKeyInternal(ctx context.Context, env *models.Environment, user *models.User) (*CreateEnvironmentOutput, error) {
	// New API key-based pairing flow
	env.Status = string(models.EnvironmentStatusPending)

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentCreationError{Err: err}).Error())
	}

	// Generate API key for environment
	apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, created.ID, user.ID)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create environment API key", "environmentID", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to create environment API key")
	}

	// Store the API key in AccessToken field (encrypted) for manager-to-agent auth
	encryptedKey := apiKeyDto.Key // Store the full key

	// Link API key to environment and store encrypted key for manager use
	updates := map[string]any{
		"api_key_id":   apiKeyDto.ID,
		"access_token": encryptedKey,
	}
	created, err = h.environmentService.UpdateEnvironment(ctx, created.ID, updates, &user.ID, &user.Username)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to link API key to environment", "environmentID", created.ID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to link API key")
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}
	h.applyEdgeRuntimeStateInternal(&out)

	return &CreateEnvironmentOutput{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data: EnvironmentWithApiKey{
				Environment: out,
				ApiKey:      new(apiKeyDto.Key),
			},
		},
	}, nil
}

func (h *EnvironmentHandler) createEnvironmentLegacyInternal(ctx context.Context, env *models.Environment, user *models.User, body environment.Create) (*CreateEnvironmentOutput, error) {
	if body.AccessToken != nil && *body.AccessToken != "" {
		env.AccessToken = body.AccessToken
	}

	created, err := h.environmentService.CreateEnvironment(ctx, env, &user.ID, &user.Username)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentCreationError{Err: err}).Error())
	}

	// Sync registries and git repositories in background (intentionally detached from request context)
	if created.AccessToken != nil && *created.AccessToken != "" {
		h.triggerEnvironmentResourceSyncInternal(ctx, created.ID, created.Name, "environment creation")
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](created)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}
	h.applyEdgeRuntimeStateInternal(&out)

	return &CreateEnvironmentOutput{
		Body: base.ApiResponse[EnvironmentWithApiKey]{
			Success: true,
			Data: EnvironmentWithApiKey{
				Environment: out,
			},
		},
	}, nil
}

// GetEnvironment returns an environment by ID.
func (h *EnvironmentHandler) GetEnvironment(ctx context.Context, input *GetEnvironmentInput) (*GetEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	env, err := h.environmentService.GetEnvironmentByID(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.EnvironmentNotFoundError{}).Error())
	}

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](env)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}
	h.applyEdgeRuntimeStateInternal(&out)
	if env.IsEdge {
		if certInfo, certErr := readGeneratedEdgeMTLSCertificateInfoInternal(h.cfg, env.ID); certErr == nil {
			out.EdgeMTLSCertificate = certInfo
		}
	}

	return &GetEnvironmentOutput{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateEnvironment updates an environment.
func (h *EnvironmentHandler) UpdateEnvironment(ctx context.Context, input *UpdateEnvironmentInput) (*UpdateEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	isLocalEnv := input.ID == localDockerEnvironmentID
	updates := h.buildUpdateMapInternal(&input.Body, isLocalEnv)

	h.handleEnvironmentPairingInternal(ctx, input.ID, &input.Body, updates, isLocalEnv)

	user, _ := humamw.GetCurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}
	updated, updateErr := h.environmentService.UpdateEnvironment(ctx, input.ID, updates, userID, username)
	if updateErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentUpdateError{Err: updateErr}).Error())
	}

	h.triggerPostUpdateTasksInternal(ctx, input.ID, updated, &input.Body)

	out, mapErr := mapper.MapOne[*models.Environment, environment.Environment](updated)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
	}
	h.applyEdgeRuntimeStateInternal(&out)

	// If regenerating API key, return the new key
	var newApiKey *string
	if input.Body.RegenerateApiKey != nil && *input.Body.RegenerateApiKey {
		user, err := requireUserInternal(ctx)
		if err != nil {
			return nil, err
		}

		// Delete existing API key if any
		if updated.ApiKeyID != nil {
			_ = h.apiKeyService.DeleteApiKey(ctx, *updated.ApiKeyID)
		}

		// Generate new API key
		apiKeyDto, err := h.apiKeyService.CreateEnvironmentApiKey(ctx, input.ID, user.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to create new environment API key", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Use service method to update environment and create event
		encryptedKey := apiKeyDto.Key
		err = h.environmentService.RegenerateEnvironmentApiKey(ctx, input.ID, apiKeyDto.ID, encryptedKey, user.ID, user.Username, updated.Name)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to regenerate API key", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to regenerate API key")
		}

		// Fetch updated environment
		updated, err = h.environmentService.GetEnvironmentByID(ctx, input.ID)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to fetch updated environment", "environmentID", input.ID, "error", err.Error())
			return nil, huma.Error500InternalServerError("Failed to fetch updated environment")
		}

		// Re-map with updated environment data
		out, mapErr = mapper.MapOne[*models.Environment, environment.Environment](updated)
		if mapErr != nil {
			return nil, huma.Error500InternalServerError((&common.EnvironmentMappingError{Err: mapErr}).Error())
		}
		h.applyEdgeRuntimeStateInternal(&out)

		newApiKey = new(apiKeyDto.Key)
	}

	// Set the API key on the response if regenerated
	out.ApiKey = newApiKey

	return &UpdateEnvironmentOutput{
		Body: base.ApiResponse[environment.Environment]{
			Success: true,
			Data:    out,
		},
	}, nil
}

func (h *EnvironmentHandler) applyEdgeRuntimeStateInternal(env *environment.Environment) {
	services.ApplyEnvironmentRuntimeState(env)
}

// DeleteEnvironment deletes an environment.
func (h *EnvironmentHandler) DeleteEnvironment(ctx context.Context, input *DeleteEnvironmentInput) (*DeleteEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == localDockerEnvironmentID {
		return nil, huma.Error400BadRequest((&common.LocalEnvironmentDeletionError{}).Error())
	}

	user, _ := humamw.GetCurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}
	if err := h.environmentService.DeleteEnvironment(ctx, input.ID, userID, username); err != nil {
		return nil, huma.Error500InternalServerError((&common.EnvironmentDeletionError{Err: err}).Error())
	}

	return &DeleteEnvironmentOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment deleted successfully",
			},
		},
	}, nil
}

// TestConnection tests connectivity to an environment.
func (h *EnvironmentHandler) TestConnection(ctx context.Context, input *TestConnectionInput) (*TestConnectionOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	var apiUrl *string
	if input.Body != nil {
		apiUrl = input.Body.ApiUrl
	}

	status, err := h.environmentService.TestConnection(ctx, input.ID, apiUrl)
	resp := environment.Test{Status: status}
	if err != nil {
		resp.Message = new(err.Error())
		return &TestConnectionOutput{
			Body: base.ApiResponse[environment.Test]{
				Success: false,
				Data:    resp,
			},
		}, err
	}

	return &TestConnectionOutput{
		Body: base.ApiResponse[environment.Test]{
			Success: true,
			Data:    resp,
		},
	}, nil
}

// UpdateHeartbeat updates the heartbeat for an environment.
func (h *EnvironmentHandler) UpdateHeartbeat(ctx context.Context, input *UpdateHeartbeatInput) (*UpdateHeartbeatOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); err != nil {
		return nil, huma.Error500InternalServerError((&common.HeartbeatUpdateError{Err: err}).Error())
	}

	return &UpdateHeartbeatOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Heartbeat updated successfully",
			},
		},
	}, nil
}

// PairAgent generates or rotates the local agent pairing token.
func (h *EnvironmentHandler) PairAgent(ctx context.Context, input *PairAgentInput) (*PairAgentOutput, error) {
	if h.environmentService == nil || h.settingsService == nil || h.cfg == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID != localDockerEnvironmentID {
		return nil, huma.Error404NotFound("Not found")
	}

	shouldRotate := input.Body != nil && input.Body.Rotate != nil && *input.Body.Rotate
	if h.cfg.AgentToken == "" || shouldRotate {
		h.cfg.AgentToken = utils.GenerateRandomString(48)
	}

	if err := h.settingsService.SetStringSetting(ctx, "agentToken", h.cfg.AgentToken); err != nil {
		return nil, huma.Error500InternalServerError((&common.AgentTokenPersistenceError{Err: err}).Error())
	}

	return &PairAgentOutput{
		Body: base.ApiResponse[environment.AgentPairResponse]{
			Success: true,
			Data: environment.AgentPairResponse{
				Token: h.cfg.AgentToken,
			},
		},
	}, nil
}

// SyncEnvironment syncs container registries and git repositories to an environment.
func (h *EnvironmentHandler) SyncEnvironment(ctx context.Context, input *SyncEnvironmentInput) (*SyncEnvironmentOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	// Sync registries
	if err := h.environmentService.SyncRegistriesToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync registries", "environmentID", input.ID, "error", err.Error())
	}

	// Sync git repositories
	if err := h.environmentService.SyncRepositoriesToEnvironment(ctx, input.ID); err != nil {
		slog.WarnContext(ctx, "Failed to sync git repositories", "environmentID", input.ID, "error", err.Error())
	}

	return &SyncEnvironmentOutput{
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

func (h *EnvironmentHandler) triggerPostUpdateTasksInternal(ctx context.Context, environmentID string, updated *models.Environment, req *environment.Update) {
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
		if err := h.environmentService.SyncRegistriesToEnvironment(syncCtx, envID); err != nil {
			slog.WarnContext(syncCtx, "Failed to sync registries to environment",
				"environmentID", envID,
				"environmentName", envName,
				"reason", syncReason,
				"error", err.Error())
		}
	}(detachedCtx, environmentID, environmentName, reason)

	go func(syncCtx context.Context, envID string, envName string, syncReason string) {
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
func (h *EnvironmentHandler) PairEnvironment(ctx context.Context, input *PairEnvironmentInput) (*PairEnvironmentOutput, error) {
	if h.environmentService == nil || h.apiKeyService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

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

	if env.Status != string(models.EnvironmentStatusPending) {
		return nil, huma.Error400BadRequest("Environment is not in pending status")
	}

	updates := map[string]any{
		"status": string(models.EnvironmentStatusOnline),
	}
	_, err = h.environmentService.UpdateEnvironment(ctx, *envID, updates, nil, nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update environment status", "environmentID", *envID, "error", err.Error())
		return nil, huma.Error500InternalServerError("Failed to complete pairing")
	}

	slog.InfoContext(ctx, "Environment pairing completed", "environmentID", *envID, "environmentName", env.Name)
	h.triggerEnvironmentResourceSyncInternal(ctx, *envID, env.Name, "environment pairing")

	return &PairEnvironmentOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Environment pairing completed successfully",
			},
		},
	}, nil
}

// GetDeploymentSnippets returns deployment snippets for an environment.
func (h *EnvironmentHandler) GetDeploymentSnippets(ctx context.Context, input *GetDeploymentSnippetsInput) (*GetDeploymentSnippetsOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

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
	var snippets *services.DeploymentSnippets
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

	return &GetDeploymentSnippetsOutput{
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
func (h *EnvironmentHandler) GetEnvironmentVersion(ctx context.Context, input *GetEnvironmentVersionInput) (*GetEnvironmentVersionOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

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
			if _, ok := edge.RequestTunnelAndWait(reqCtx, input.ID, edge.DefaultTunnelDemandTTL, edge.DefaultTunnelAcquireTimeout()); !ok {
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
		validatedURL, validateErr := httputils.ValidateOutboundHTTPURL(env.ApiUrl)
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

		if err := json.NewDecoder(resp.Body).Decode(&versionInfo); err != nil {
			return nil, huma.Error500InternalServerError("Failed to decode version response")
		}
	}

	// Update environment status to online since we successfully contacted it
	if updateErr := h.environmentService.UpdateEnvironmentHeartbeat(ctx, input.ID); updateErr != nil {
		slog.WarnContext(ctx, "Failed to update environment heartbeat", "environment_id", input.ID, "error", updateErr)
		// Don't fail the request if heartbeat update fails
	}

	return &GetEnvironmentVersionOutput{
		Body: base.ApiResponse[version.Info]{
			Success: true,
			Data:    versionInfo,
		},
	}, nil
}

// DownloadEdgeMTLSCA downloads the Arcane-managed edge mTLS CA certificate.
func (h *EnvironmentHandler) DownloadEdgeMTLSCA(ctx context.Context, _ *DownloadEdgeMTLSCAInput) (*huma.StreamResponse, error) {
	caPath, err := generatedEdgeMTLSCAPathInternal(h.cfg)
	if err != nil {
		return nil, huma.Error404NotFound("Arcane-managed edge mTLS CA is not available")
	}

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
			h.logMTLSAuditEventInternal(humaCtx.Context(), nil, models.EventTypeEnvironmentMTLSDownload,
				"mTLS CA downloaded",
				fmt.Sprintf("Administrator downloaded edge mTLS CA %q", fileName),
				models.JSON{
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
			h.logMTLSAuditEventInternal(humaCtx.Context(), env, models.EventTypeEnvironmentMTLSDownload,
				"mTLS bundle downloaded",
				fmt.Sprintf("Administrator downloaded edge mTLS bundle %q (%d files)", fileName, len(files)),
				models.JSON{
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
			h.logMTLSAuditEventInternal(humaCtx.Context(), env, models.EventTypeEnvironmentMTLSDownload,
				"mTLS asset downloaded",
				fmt.Sprintf("Administrator downloaded edge mTLS asset %q", file.Name),
				models.JSON{
					"fileName":  file.Name,
					"kind":      "file",
					"sensitive": isSensitiveMTLSAssetNameInternal(file.Name),
				})
		},
	}, nil
}

func (h *EnvironmentHandler) loadEnvironmentMTLSEnvironmentInternal(ctx context.Context, environmentID string) (*models.Environment, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

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

func (h *EnvironmentHandler) loadEnvironmentMTLSFilesInternal(ctx context.Context, environmentID string) (*models.Environment, []services.DeploymentSnippetFile, error) {
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

func (h *EnvironmentHandler) loadEnvironmentMTLSFileInternal(ctx context.Context, environmentID string, fileName string) (*models.Environment, services.DeploymentSnippetFile, error) {
	env, files, err := h.loadEnvironmentMTLSFilesInternal(ctx, environmentID)
	if err != nil {
		return nil, services.DeploymentSnippetFile{}, err
	}

	for _, file := range files {
		if file.Name == fileName {
			return env, file, nil
		}
	}

	return nil, services.DeploymentSnippetFile{}, huma.Error404NotFound("Requested mTLS asset was not found")
}

// isSensitiveMTLSAssetNameInternal reports whether the given generated asset
// filename contains secret material (currently just the agent private key).
// Sensitive asset contents must not be returned inline in JSON responses; the
// client should fetch them via the admin-only download endpoint instead.
func isSensitiveMTLSAssetNameInternal(fileName string) bool {
	name := strings.ToLower(strings.TrimSpace(fileName))
	return strings.HasSuffix(name, ".key") || strings.HasSuffix(name, "-key.pem") || strings.HasSuffix(name, "_key.pem")
}

func environmentMTLSDownloadBaseNameInternal(env *models.Environment) string {
	base := strings.TrimSpace(env.Name)
	if base == "" {
		base = "environment"
	}

	base = strings.Map(func(r rune) rune {
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
	}, base)

	base = strings.Trim(base, "-")
	if base == "" {
		base = "environment"
	}

	return base + "-" + env.ID
}

func environmentMTLSAssetDownloadNameInternal(env *models.Environment, fileName string) string {
	base := environmentMTLSDownloadBaseNameInternal(env)

	switch fileName {
	case "agent.crt":
		return base + ".pem"
	case "agent.key":
		return base + ".key"
	default:
		return fileName
	}
}

func environmentMTLSAssetFileModeInternal(file services.DeploymentSnippetFile) os.FileMode {
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
func (h *EnvironmentHandler) logMTLSAuditEventInternal(ctx context.Context, env *models.Environment, eventType models.EventType, title, description string, extra models.JSON) {
	if h == nil || h.eventService == nil {
		return
	}

	user, _ := humamw.GetCurrentUserFromContext(ctx)
	var userID, username *string
	if user != nil {
		userID = new(user.ID)
		username = new(user.Username)
	}

	if extra == nil {
		extra = models.JSON{}
	}
	if remoteAddr := strings.TrimSpace(humamw.GetRemoteAddrFromContext(ctx)); remoteAddr != "" {
		extra["remoteAddr"] = remoteAddr
	}

	req := services.CreateEventRequest{
		Type:        eventType,
		Severity:    models.EventSeverityInfo,
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
