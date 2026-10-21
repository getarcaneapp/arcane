package secrets

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
)

type Handler struct {
	service *Service
	audit   func(ctx context.Context, environmentID, action, resourceType, resourceName string, metadata map[string]any)
}

func NewHandler(service *Service, audit func(ctx context.Context, environmentID, action, resourceType, resourceName string, metadata map[string]any)) *Handler {
	return &Handler{service: service, audit: audit}
}

type ListSecretsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret name or ID"`
}

type CreateSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.PodmanSecretCreateRequest
}

type UpdateSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret name"`
	Body          types.PodmanSecretUpdateRequest
}

type DeleteSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret name or ID"`
}

func (h *Handler) List(ctx context.Context, input *ListSecretsInput) (*handlerutil.Page[types.PodmanSecret], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list Podman secrets: " + err.Error())
		}
		return &handlerutil.Page[types.PodmanSecret]{Body: body}, nil
	}

	items, page, err := h.service.ListPaginated(ctx, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list Podman secrets: "+apiErr.Message)
	}
	return &handlerutil.Page[types.PodmanSecret]{
		Body: types.Paginated[types.PodmanSecret]{
			Success:    true,
			Data:       kit.Ternary(items != nil, items, []types.PodmanSecret{}),
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *Handler) Get(ctx context.Context, input *GetSecretInput) (*handlerutil.Out[types.PodmanSecret], error) {
	secret, err := h.service.Inspect(ctx, input.SecretID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to inspect Podman secret: "+apiErr.Message)
	}
	return &handlerutil.Out[types.PodmanSecret]{Body: types.ApiResponse[types.PodmanSecret]{Success: true, Data: secret}}, nil
}

func (h *Handler) Create(ctx context.Context, input *CreateSecretInput) (*handlerutil.Out[types.PodmanSecretCreateResponse], error) {
	id, err := h.service.Create(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create Podman secret: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "secret.create", "secret", input.Body.Name, map[string]any{"secretId": id})
	return &handlerutil.Out[types.PodmanSecretCreateResponse]{Body: types.ApiResponse[types.PodmanSecretCreateResponse]{Success: true, Data: types.PodmanSecretCreateResponse{ID: id}}}, nil
}

func (h *Handler) Update(ctx context.Context, input *UpdateSecretInput) (*handlerutil.Out[types.PodmanSecret], error) {
	secret, err := h.service.Update(ctx, input.SecretID, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update Podman secret: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "secret.update", "secret", input.SecretID, map[string]any{"secretId": secret.ID})
	return &handlerutil.Out[types.PodmanSecret]{Body: types.ApiResponse[types.PodmanSecret]{Success: true, Data: secret}}, nil
}

func (h *Handler) Delete(ctx context.Context, input *DeleteSecretInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.Delete(ctx, input.SecretID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to remove Podman secret: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "secret.delete", "secret", input.SecretID, nil)
	return handlerutil.MessageOutput("Podman secret removed", ""), nil
}
