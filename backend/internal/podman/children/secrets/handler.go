package secrets

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
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
}

type CreateSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          podmantypes.SecretCreateRequest
}

type DeleteSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret name or ID"`
}

func (h *Handler) List(ctx context.Context, _ *ListSecretsInput) (*handlerutil.Out[[]podmantypes.Secret], error) {
	items, err := h.service.List(ctx)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list Podman secrets: "+apiErr.Message)
	}
	return &handlerutil.Out[[]podmantypes.Secret]{
		Body: base.ApiResponse[[]podmantypes.Secret]{
			Success: true,
			Data:    kit.Ternary(items != nil, items, []podmantypes.Secret{}),
		},
	}, nil
}

func (h *Handler) Create(ctx context.Context, input *CreateSecretInput) (*handlerutil.Out[podmantypes.SecretCreateResponse], error) {
	id, err := h.service.Create(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create Podman secret: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "secret.create", "secret", input.Body.Name, map[string]any{"secretId": id})
	return &handlerutil.Out[podmantypes.SecretCreateResponse]{Body: base.ApiResponse[podmantypes.SecretCreateResponse]{Success: true, Data: podmantypes.SecretCreateResponse{ID: id}}}, nil
}

func (h *Handler) Delete(ctx context.Context, input *DeleteSecretInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.service.Delete(ctx, input.SecretID); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to remove Podman secret: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "secret.delete", "secret", input.SecretID, nil)
	return handlerutil.MessageOutput("Podman secret removed", ""), nil
}
