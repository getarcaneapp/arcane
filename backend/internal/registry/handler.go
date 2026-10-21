package registry

import (
	"context"
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// ContainerRegistryHandler handles container registry management endpoints.
type ContainerRegistryHandler struct {
	registryService      *ContainerRegistryService
	syncRemoteRegistries func(context.Context)
}

type ListContainerRegistriesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
}

type CreateContainerRegistryInput struct {
	types.Create[types.ContainerRegistry]
}

type GetContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type UpdateContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
	types.Update[types.ContainerRegistry]
}

type DeleteContainerRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type ContainerRegistryActionInput struct {
	ID     string `path:"id" doc:"Registry ID"`
	Action string `query:"action" required:"true" enum:"test" doc:"Registry action"`
}

type SyncContainerRegistriesInput struct {
	Body types.ContainerRegistrySyncRequest
}

// NewHandler builds the container registry HTTP handler.
func NewHandler(registryService *ContainerRegistryService, syncRemoteRegistries func(context.Context)) *ContainerRegistryHandler {
	return &ContainerRegistryHandler{registryService: registryService, syncRemoteRegistries: syncRemoteRegistries}
}

// ListRegistries returns a paginated list of container registries.
func (h *ContainerRegistryHandler) ListRegistries(ctx context.Context, input *ListContainerRegistriesInput) (*handlerutil.Page[types.ContainerRegistry], error) {
	registries, paginationResp, err := h.registryService.GetRegistriesPaginated(ctx, handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search))
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list registries: " + err.Error())
	}

	return &handlerutil.Page[types.ContainerRegistry]{
		Body: types.Paginated[types.ContainerRegistry]{
			Success:    true,
			Data:       registries,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetPullUsage returns pull usage visibility for configured registries.
func (h *ContainerRegistryHandler) GetPullUsage(ctx context.Context, input *struct{}) (*handlerutil.Out[types.ContainerRegistryPullUsageResponse], error) {
	usage, err := h.registryService.GetRegistryPullUsage(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to retrieve registry: " + err.Error())
	}

	return &handlerutil.Out[types.ContainerRegistryPullUsageResponse]{
		Body: types.ApiResponse[types.ContainerRegistryPullUsageResponse]{
			Success: true,
			Data:    usage,
		},
	}, nil
}

// CreateRegistry creates a new container registry.
func (h *ContainerRegistryHandler) CreateRegistry(ctx context.Context, input *CreateContainerRegistryInput) (*handlerutil.Out[types.ContainerRegistry], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}

	reg, err := h.registryService.CreateRegistry(ctx, input.Body, submitted)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create registry: "+err.Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry creation")

	body, mapErr := handlerutil.MapOneAPIResponse[*ContainerRegistry, types.ContainerRegistry](reg, func(error) string {
		return "Failed to map registry"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[types.ContainerRegistry]{Body: body}, nil
}

// GetRegistry returns a container registry by ID.
func (h *ContainerRegistryHandler) GetRegistry(ctx context.Context, input *GetContainerRegistryInput) (*handlerutil.Out[types.ContainerRegistry], error) {
	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve registry: "+err.Error())
	}

	body, mapErr := handlerutil.MapOneAPIResponse[*ContainerRegistry, types.ContainerRegistry](reg, func(error) string {
		return "Failed to map registry"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[types.ContainerRegistry]{Body: body}, nil
}

// UpdateRegistry updates a container registry.
func (h *ContainerRegistryHandler) UpdateRegistry(ctx context.Context, input *UpdateContainerRegistryInput) (*handlerutil.Out[types.ContainerRegistry], error) {
	submitted, err := input.SubmittedFields()
	if err != nil {
		return nil, huma.Error400BadRequest("invalid request body", err)
	}

	reg, err := h.registryService.UpdateRegistry(ctx, input.ID, input.Body, submitted)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update registry: "+err.Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry update")

	body, mapErr := handlerutil.MapOneAPIResponse[*ContainerRegistry, types.ContainerRegistry](reg, func(error) string {
		return "Failed to map registry"
	})
	if mapErr != nil {
		return nil, mapErr
	}

	return &handlerutil.Out[types.ContainerRegistry]{Body: body}, nil
}

// DeleteRegistry deletes a container registry.
func (h *ContainerRegistryHandler) DeleteRegistry(ctx context.Context, input *DeleteContainerRegistryInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.registryService.DeleteRegistry(ctx, input.ID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to delete registry: "+err.Error())
	}

	h.triggerRemoteRegistrySync(ctx, "registry deletion")

	return handlerutil.MessageOutput("Container registry deleted successfully", ""), nil
}

// TestRegistry tests connectivity to a container registry.
func (h *ContainerRegistryHandler) TestRegistry(ctx context.Context, input *ContainerRegistryActionInput) (*handlerutil.Out[types.MessageResponse], error) {
	reg, err := h.registryService.GetRegistryByID(ctx, input.ID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to retrieve registry: "+err.Error())
	}

	// ECR registries use a different auth flow: generate a temporary token via AWS API.
	if reg.RegistryType == RegistryTypeECR {
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

	return handlerutil.MessageOutput(kit.Ternary(
		strings.TrimSpace(reg.Username) == "" && strings.TrimSpace(decryptedToken) == "",
		"Registry saved (no credentials to test)",
		"Authentication succeeded",
	), ""), nil
}

// SyncRegistries syncs container registries from a remote source.
func (h *ContainerRegistryHandler) SyncRegistries(ctx context.Context, input *SyncContainerRegistriesInput) (*handlerutil.Out[types.MessageResponse], error) {
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

	slog.DebugContext(ctx, "Queueing registry sync to remote environments", "reason", reason)
	h.syncRemoteRegistries(ctx)
}
