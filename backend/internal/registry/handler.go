package registry

import (
	"context"
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/containerregistry"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

const registryRemoteSyncFailureMessageInternal = "Failed to fan out registry sync to remote environments"

// ContainerRegistryHandler handles container registry management endpoints.
type ContainerRegistryHandler struct {
	registryService      *ContainerRegistryService
	syncRemoteRegistries func(context.Context) error
}

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

// NewHandler builds the container registry HTTP handler.
func NewHandler(registryService *ContainerRegistryService, syncRemoteRegistries func(context.Context) error) *ContainerRegistryHandler {
	return &ContainerRegistryHandler{registryService: registryService, syncRemoteRegistries: syncRemoteRegistries}
}

// ListRegistries returns a paginated list of container registries.
func (h *ContainerRegistryHandler) ListRegistries(ctx context.Context, input *ListContainerRegistriesInput) (*handlerutil.Page[containerregistry.ContainerRegistry], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)

	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list registries: " + err.Error())
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
		return nil, huma.Error500InternalServerError("Failed to retrieve registry: " + err.Error())
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
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create registry: "+err.Error())
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
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve registry: "+err.Error())
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
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update registry: "+err.Error())
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
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete registry: "+err.Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry deletion")

	return handlerutil.MessageOutput("Container registry deleted successfully", ""), nil
}

// TestRegistry tests connectivity to a container registry.
func (h *ContainerRegistryHandler) TestRegistry(ctx context.Context, input *TestContainerRegistryInput) (*handlerutil.Out[base.MessageResponse], error) {
	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve registry: "+err.Error())
	}

	// ECR registries use a different auth flow: generate a temporary token via AWS API.
	if reg.RegistryType == "ecr" {
		if testECRRegistryErr := h.registryService.TestECRRegistry(ctx, reg); testECRRegistryErr != nil {
			return nil, huma.Error400BadRequest("Registry test failed: " + testECRRegistryErr.Error())
		}
		return handlerutil.MessageOutput("ECR authentication succeeded", ""), nil
	}

	decryptedToken, err := crypto.Decrypt(reg.Token)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to decrypt token: " + err.Error())
	}

	if testRegistryErr := h.registryService.TestRegistry(ctx, reg.URL, reg.Username, decryptedToken); testRegistryErr != nil {
		return nil, huma.Error400BadRequest("Registry test failed: " + testRegistryErr.Error())
	}

	noCredentials := strings.TrimSpace(reg.Username) == "" && strings.TrimSpace(decryptedToken) == ""
	msg := kit.Ternary(noCredentials, "Registry saved (no credentials to test)", "Authentication succeeded")

	return handlerutil.MessageOutput(msg, ""), nil
}

// SyncRegistries syncs container registries from a remote source.
func (h *ContainerRegistryHandler) SyncRegistries(ctx context.Context, input *SyncContainerRegistriesInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.registryService.SyncRegistries(ctx, input.Body.Registries); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to sync registries: "+err.Error())
	}

	return handlerutil.MessageOutput("Registries synced successfully", ""), nil
}

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
