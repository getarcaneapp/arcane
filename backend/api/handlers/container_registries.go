package handlers

import (
	"context"
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
)

// containerRegistryHandler handles container registry management endpoints.
type containerRegistryHandler struct {
	registryService    *services.ContainerRegistryService
	environmentService *services.EnvironmentService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// containerRegistryPaginatedResponse is the paginated response for container registries.
type containerRegistryPaginatedResponse struct {
	Success    bool                                  `json:"success"`
	Data       []containerregistry.ContainerRegistry `json:"data"`
	Pagination base.PaginationResponse               `json:"pagination"`
}

type listContainerRegistriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type listContainerRegistriesOutput struct {
	Body containerRegistryPaginatedResponse
}

type createContainerRegistryInput struct {
	Body models.CreateContainerRegistryRequest
}

type createContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type getContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type getContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type updateContainerRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body models.UpdateContainerRegistryRequest
}

type updateContainerRegistryOutput struct {
	Body base.ApiResponse[containerregistry.ContainerRegistry]
}

type deleteContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type deleteContainerRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type testContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type testContainerRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type getContainerRegistryPullUsageOutput struct {
	Body base.ApiResponse[containerregistry.PullUsageResponse]
}

type syncContainerRegistriesInput struct {
	Body containerregistry.SyncRequest
}

type syncContainerRegistriesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterContainerRegistries registers all container registry endpoints.
func RegisterContainerRegistries(api huma.API, registryService *services.ContainerRegistryService, environmentService *services.EnvironmentService) {
	h := &containerRegistryHandler{
		registryService:    registryService,
		environmentService: environmentService,
	}

	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistries",
		Method:      "GET",
		Path:        "/container-registries",
		Summary:     "List container registries",
		Description: "Get a paginated list of container registries",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesList),
	}, h.listRegistriesInternal)

	huma.Register(api, huma.Operation{
		OperationID: "createContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries",
		Summary:     "Create a container registry",
		Description: "Create a new container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesCreate),
	}, h.createRegistryInternal)

	huma.Register(api, huma.Operation{
		OperationID: "syncContainerRegistries",
		Method:      "POST",
		Path:        "/container-registries/sync",
		Summary:     "Sync container registries",
		Description: "Sync container registries from a remote source",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.syncRegistriesInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistryPullUsage",
		Method:      "GET",
		Path:        "/container-registries/pull-usage",
		Summary:     "Get container registry pull usage",
		Description: "Get configured registry pull usage and rate limit visibility",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.getPullUsageInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistry",
		Method:      "GET",
		Path:        "/container-registries/{id}",
		Summary:     "Get a container registry",
		Description: "Get a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesRead),
	}, h.getRegistryInternal)

	huma.Register(api, huma.Operation{
		OperationID: "updateContainerRegistry",
		Method:      "PUT",
		Path:        "/container-registries/{id}",
		Summary:     "Update a container registry",
		Description: "Update an existing container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.updateRegistryInternal)

	huma.Register(api, huma.Operation{
		OperationID: "deleteContainerRegistry",
		Method:      "DELETE",
		Path:        "/container-registries/{id}",
		Summary:     "Delete a container registry",
		Description: "Delete a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesDelete),
	}, h.deleteRegistryInternal)

	huma.Register(api, huma.Operation{
		OperationID: "testContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries/{id}/test",
		Summary:     "Test a container registry",
		Description: "Test connectivity and authentication to a container registry",
		Tags:        []string{"Container Registries"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermRegistriesTest),
	}, h.testRegistryInternal)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRegistries returns a paginated list of container registries.
func (h *containerRegistryHandler) listRegistriesInternal(ctx context.Context, input *listContainerRegistriesInput) (*listContainerRegistriesOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryListError{Err: err}).Error())
	}

	return &listContainerRegistriesOutput{
		Body: containerRegistryPaginatedResponse{
			Success:    true,
			Data:       registries,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetPullUsage returns pull usage visibility for configured registries.
func (h *containerRegistryHandler) getPullUsageInternal(ctx context.Context, _ *struct{}) (*getContainerRegistryPullUsageOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	usage, err := h.registryService.GetRegistryPullUsage(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryRetrievalError{Err: err}).Error())
	}

	return &getContainerRegistryPullUsageOutput{
		Body: base.ApiResponse[containerregistry.PullUsageResponse]{
			Success: true,
			Data:    usage,
		},
	}, nil
}

// CreateRegistry creates a new container registry.
func (h *containerRegistryHandler) createRegistryInternal(ctx context.Context, input *createContainerRegistryInput) (*createContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.CreateRegistry(ctx, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryCreationError{Err: err}).Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry creation")

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &createContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetRegistry returns a container registry by ID.
func (h *containerRegistryHandler) getRegistryInternal(ctx context.Context, input *getContainerRegistryInput) (*getContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryRetrievalError{Err: err}).Error())
	}

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &getContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRegistry updates a container registry.
func (h *containerRegistryHandler) updateRegistryInternal(ctx context.Context, input *updateContainerRegistryInput) (*updateContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.UpdateRegistry(ctx, input.ID, input.Body)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryUpdateError{Err: err}).Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry update")

	out, mapErr := mapper.MapOne[*models.ContainerRegistry, containerregistry.ContainerRegistry](reg)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &updateContainerRegistryOutput{
		Body: base.ApiResponse[containerregistry.ContainerRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteRegistry deletes a container registry.
func (h *containerRegistryHandler) deleteRegistryInternal(ctx context.Context, input *deleteContainerRegistryInput) (*deleteContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.registryService.DeleteRegistry(ctx, input.ID); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryDeletionError{Err: err}).Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry deletion")

	return &deleteContainerRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Container registry deleted successfully",
			},
		},
	}, nil
}

// TestRegistry tests connectivity to a container registry.
func (h *containerRegistryHandler) testRegistryInternal(ctx context.Context, input *testContainerRegistryInput) (*testContainerRegistryOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistryRetrievalError{Err: err}).Error())
	}

	// ECR registries use a different auth flow: generate a temporary token via AWS API.
	if reg.RegistryType == "ecr" {
		if err := h.registryService.TestECRRegistry(ctx, reg); err != nil {
			return nil, huma.Error400BadRequest((&common.RegistryTestError{Err: err}).Error())
		}
		return &testContainerRegistryOutput{
			Body: base.ApiResponse[base.MessageResponse]{
				Success: true,
				Data: base.MessageResponse{
					Message: "ECR authentication succeeded",
				},
			},
		}, nil
	}

	decryptedToken, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TokenDecryptionError{Err: err}).Error())
	}

	if err := h.registryService.TestRegistry(ctx, reg.URL, reg.Username, decryptedToken); err != nil {
		return nil, huma.Error400BadRequest((&common.RegistryTestError{Err: err}).Error())
	}

	msg := "Authentication succeeded"
	if strings.TrimSpace(reg.Username) == "" && strings.TrimSpace(decryptedToken) == "" {
		msg = "Registry saved (no credentials to test)"
	}

	return &testContainerRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: msg,
			},
		},
	}, nil
}

// SyncRegistries syncs container registries from a remote source.
func (h *containerRegistryHandler) syncRegistriesInternal(ctx context.Context, input *syncContainerRegistriesInput) (*syncContainerRegistriesOutput, error) {
	if h.registryService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.registryService.SyncRegistries(ctx, input.Body.Registries); err != nil {
		apiErr := models.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), (&common.RegistrySyncError{Err: err}).Error())
	}

	return &syncContainerRegistriesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registries synced successfully",
			},
		},
	}, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

func (h *containerRegistryHandler) triggerRemoteRegistrySync(ctx context.Context, reason string) {
	if h.environmentService == nil {
		return
	}

	detachedCtx := context.WithoutCancel(ctx)

	go func(syncCtx context.Context, syncReason string) {
		if err := h.environmentService.SyncRegistriesToRemoteEnvironments(syncCtx); err != nil {
			slog.WarnContext(syncCtx, "Failed to fan out registry sync to remote environments", "reason", syncReason, "error", err.Error())
		}
	}(detachedCtx, reason)
}
