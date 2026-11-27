package stats

import (
	"cmp"
	"context"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/timeouts"
	"getarcane.dev/types/v3"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(ctx context.Context, _ *types.EnvironmentInput) (*handlerutil.Out[types.BackupStorageStats], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to load backup storage stats: " + err.Error())
		}
		return &handlerutil.Out[types.BackupStorageStats]{Body: body}, nil
	}
	stats, err := h.service.Get(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load backup storage stats: " + err.Error())
	}
	return &handlerutil.Out[types.BackupStorageStats]{Body: types.ApiResponse[types.BackupStorageStats]{Success: true, Data: stats}}, nil
}

func (h *Handler) Refresh(ctx context.Context, _ *types.EnvironmentInput) (*handlerutil.Out[types.BackupStorageStats], error) {
	if scope, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(httpx.WithEnvironmentScope(ctx, &httpx.EnvironmentScope{ //exhaustruct:enforce
			Handler: scope.Handler,
			Request: scope.Request,
			Resolve: scope.Resolve,
			Path:    scope.Path,
			IDs:     scope.IDs,
			Body:    scope.Body,
			Timeout: cmp.Or(scope.Timeout, timeouts.DefaultAgentProxy),
		}))
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to refresh backup storage stats: " + err.Error())
		}
		return &handlerutil.Out[types.BackupStorageStats]{Body: body}, nil
	}
	stats, err := h.service.Refresh(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to refresh backup storage stats: " + err.Error())
	}
	return &handlerutil.Out[types.BackupStorageStats]{Body: types.ApiResponse[types.BackupStorageStats]{Success: true, Data: stats}}, nil
}
