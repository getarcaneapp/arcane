package transfer

import (
	"context"

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
