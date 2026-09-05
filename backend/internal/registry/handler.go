package registry

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"

	"context"
	"log/slog"
	"strings"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	"go.getarcane.app/sys/crypto"
)

const registryRemoteSyncFailureMessageInternal = "Failed to fan out registry sync to remote environments"

// ContainerRegistryHandler handles container registry management endpoints.
type ContainerRegistryHandler struct {
	registryService      *ContainerRegistryService
	syncRemoteRegistries func(context.Context) error
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListContainerRegistriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type CreateContainerRegistryInput struct {
	Body containerregistry.CreateContainerRegistryRequest
}

type GetContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type UpdateContainerRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body containerregistry.UpdateContainerRegistryRequest
}

type DeleteContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type TestContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type SyncContainerRegistriesInput struct {
	Body containerregistry.SyncRequest
}

// ============================================================================
// Registration
// ============================================================================

// NewHandler builds the container registry HTTP handler.
func NewHandler(registryService *ContainerRegistryService, syncRemoteRegistries func(context.Context) error) *ContainerRegistryHandler {
	return &ContainerRegistryHandler{registryService: registryService, syncRemoteRegistries: syncRemoteRegistries}
}

func RegisterContainerRegistries(api huma.API, h *ContainerRegistryHandler) {
	huma.Register(api, huma.Operation{
		OperationID: "listContainerRegistries",
		Method:      "GET",
		Path:        "/container-registries",
		Summary:     "List container registries",
		Description: "Get a paginated list of container registries",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesList),
	}, h.ListRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "createContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries",
		Summary:     "Create a container registry",
		Description: "Create a new container registry",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesCreate),
	}, h.CreateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "syncContainerRegistries",
		Method:      "POST",
		Path:        "/container-registries/sync",
		Summary:     "Sync container registries",
		Description: "Sync container registries from a remote source",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.SyncRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistryPullUsage",
		Method:      "GET",
		Path:        "/container-registries/pull-usage",
		Summary:     "Get container registry pull usage",
		Description: "Get configured registry pull usage and rate limit visibility",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetPullUsage)

	huma.Register(api, huma.Operation{
		OperationID: "getContainerRegistry",
		Method:      "GET",
		Path:        "/container-registries/{id}",
		Summary:     "Get a container registry",
		Description: "Get a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesRead),
	}, h.GetRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "updateContainerRegistry",
		Method:      "PUT",
		Path:        "/container-registries/{id}",
		Summary:     "Update a container registry",
		Description: "Update an existing container registry",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesUpdate),
	}, h.UpdateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "deleteContainerRegistry",
		Method:      "DELETE",
		Path:        "/container-registries/{id}",
		Summary:     "Delete a container registry",
		Description: "Delete a container registry by ID",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesDelete),
	}, h.DeleteRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "testContainerRegistry",
		Method:      "POST",
		Path:        "/container-registries/{id}/test",
		Summary:     "Test a container registry",
		Description: "Test connectivity and authentication to a container registry",
		Tags:        []string{"Container Registries"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermRegistriesTest),
	}, h.TestRegistry)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListRegistries returns a paginated list of container registries.
func (h *ContainerRegistryHandler) ListRegistries(ctx context.Context, input *ListContainerRegistriesInput) (*handlerutil.Page[containerregistry.ContainerRegistry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to list registries").Error())
	}

	return &handlerutil.Page[containerregistry.ContainerRegistry]{
		Body: base.Paginated[containerregistry.ContainerRegistry]{
			Success:    true,
			Data:       registries,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetPullUsage returns pull usage visibility for configured registries.
func (h *ContainerRegistryHandler) GetPullUsage(ctx context.Context, input *struct{}) (*handlerutil.Out[containerregistry.PullUsageResponse], error) {
	usage, err := h.registryService.GetRegistryPullUsage(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to retrieve registry").Error())
	}

	return &handlerutil.Out[containerregistry.PullUsageResponse]{
		Body: base.ApiResponse[containerregistry.PullUsageResponse]{
			Success: true,
			Data:    usage,
		},
	}, nil
}

// CreateRegistry creates a new container registry.
func (h *ContainerRegistryHandler) CreateRegistry(ctx context.Context, input *CreateContainerRegistryInput) (*handlerutil.Out[containerregistry.ContainerRegistry], error) {
	reg, err := h.registryService.CreateRegistry(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to create registry").Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry creation")

	body, mapErr := handlerutil.MapOneAPIResponse[*ContainerRegistry, containerregistry.ContainerRegistry](reg, func(error) string {
		return "Failed to map registry"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[containerregistry.ContainerRegistry]{Body: body}, nil
}

// GetRegistry returns a container registry by ID.
func (h *ContainerRegistryHandler) GetRegistry(ctx context.Context, input *GetContainerRegistryInput) (*handlerutil.Out[containerregistry.ContainerRegistry], error) {
	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to retrieve registry").Error())
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*ContainerRegistry, containerregistry.ContainerRegistry](reg, func(error) string {
		return "Failed to map registry"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[containerregistry.ContainerRegistry]{Body: body}, nil
}

// UpdateRegistry updates a container registry.
func (h *ContainerRegistryHandler) UpdateRegistry(ctx context.Context, input *UpdateContainerRegistryInput) (*handlerutil.Out[containerregistry.ContainerRegistry], error) {
	reg, err := h.registryService.UpdateRegistry(ctx, input.ID, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to update registry").Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry update")

	body, mapErr := handlerutil.MapOneAPIResponse[*ContainerRegistry, containerregistry.ContainerRegistry](reg, func(error) string {
		return "Failed to map registry"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[containerregistry.ContainerRegistry]{Body: body}, nil
}

// DeleteRegistry deletes a container registry.
func (h *ContainerRegistryHandler) DeleteRegistry(ctx context.Context, input *DeleteContainerRegistryInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.registryService.DeleteRegistry(ctx, input.ID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to delete registry").Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry deletion")

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Container registry deleted successfully",
			},
		},
	}, nil
}

// TestRegistry tests connectivity to a container registry.
func (h *ContainerRegistryHandler) TestRegistry(ctx context.Context, input *TestContainerRegistryInput) (*handlerutil.Out[base.MessageResponse], error) {
	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to retrieve registry").Error())
	}

	// ECR registries use a different auth flow: generate a temporary token via AWS API.
	if reg.RegistryType == "ecr" {
		if err := h.registryService.TestECRRegistry(ctx, reg); err != nil {
			return nil, huma.Error400BadRequest(errors.WithMessage(err, "Registry test failed").Error())
		}
		return &handlerutil.Out[base.MessageResponse]{
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
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to decrypt token").Error())
	}

	if err := h.registryService.TestRegistry(ctx, reg.URL, reg.Username, decryptedToken); err != nil {
		return nil, huma.Error400BadRequest(errors.WithMessage(err, "Registry test failed").Error())
	}

	msg := "Authentication succeeded"
	if strings.TrimSpace(reg.Username) == "" && strings.TrimSpace(decryptedToken) == "" {
		msg = "Registry saved (no credentials to test)"
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: msg,
			},
		},
	}, nil
}

// SyncRegistries syncs container registries from a remote source.
func (h *ContainerRegistryHandler) SyncRegistries(ctx context.Context, input *SyncContainerRegistriesInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.registryService.SyncRegistries(ctx, input.Body.Registries); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), errors.WithMessage(err, "Failed to sync registries").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
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

func (h *ContainerRegistryHandler) triggerRemoteRegistrySync(ctx context.Context, reason string) {
	if h.syncRemoteRegistries == nil {
		return
	}

	detachedCtx := context.WithoutCancel(ctx)

	go func(syncCtx context.Context, syncReason string) {
		if err := h.syncRemoteRegistries(syncCtx); err != nil {
			slog.WarnContext(syncCtx, registryRemoteSyncFailureMessageInternal, "reason", syncReason, "error", err.Error())
		}
	}(detachedCtx, reason)
}
