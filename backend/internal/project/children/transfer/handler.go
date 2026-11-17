package transfer

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service *Service
	}

	TransferInput struct {
		EnvironmentID string `path:"id" doc:"Source environment ID"`
		ProjectID     string `path:"projectId" doc:"Source project ID"`
		Body          types.ProjectTransferRequest
	}

	ResourceInput struct {
		EnvironmentID string `path:"id" doc:"Source environment ID"`
		ProjectID     string `path:"projectId" doc:"Source project ID"`
		Body          types.ProjectTransferResource
	}
)

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Preflight(ctx context.Context, input *TransferInput) (*handlerutil.Out[types.ProjectTransferPreflight], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := h.service.Preflight(ctx, Options{
		User:                *user,
		SourceEnvironmentID: input.EnvironmentID,
		ProjectID:           input.ProjectID,
		Request:             input.Body,
	})
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.ProjectTransferPreflight]{
		Body: types.ApiResponse[types.ProjectTransferPreflight]{
			Success: true,
			Data:    plan,
		},
	}, nil
}

func (h *Handler) Start(ctx context.Context, input *TransferInput) (*handlerutil.Out[types.ProjectTransferStarted], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	started, err := h.service.Start(ctx, Options{
		User:                *user,
		SourceEnvironmentID: input.EnvironmentID,
		ProjectID:           input.ProjectID,
		Request:             input.Body,
	})
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.ProjectTransferStarted]{
		Body: types.ApiResponse[types.ProjectTransferStarted]{
			Success: true,
			Data:    started,
		},
	}, nil
}

func (h *Handler) CreateResource(ctx context.Context, input *ResourceInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if createErr := h.service.CreateResource(ctx, ResourceOptions{
		User:                *user,
		SourceEnvironmentID: input.EnvironmentID,
		ProjectID:           input.ProjectID,
		Request:             input.Body,
	}); createErr != nil {
		apiErr := common.ToAPIError(common.ClassifyDockerError(createErr))
		return nil, huma.NewError(apiErr.HTTPStatus(), createErr.Error())
	}
	return &handlerutil.Out[types.MessageResponse]{
		Body: types.ApiResponse[types.MessageResponse]{
			Success: true,
			Data:    types.MessageResponse{Message: fmt.Sprintf("Created %s %s on the target environment", input.Body.Kind, input.Body.Name)},
		},
	}, nil
}
