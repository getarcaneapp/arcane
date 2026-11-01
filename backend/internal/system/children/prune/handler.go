package prune

import (
	"context"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"

	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	// Handler serves the system prune endpoint.
	Handler struct {
		service *Service
		appCtx  context.Context
	}

	PruneAllInput struct {
		Body          types.PruneAllRequest `doc:"Prune options"`
		EnvironmentID string                `path:"id" doc:"Environment ID"`
	}
)

func NewHandler(service *Service, appCtx context.Context) *Handler {
	return &Handler{service: service, appCtx: appCtx}
}

func (h *Handler) GetDiskUsage(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[types.SystemDiskUsage], error) {
	usage, err := h.service.GetDiskUsage(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get disk usage: " + err.Error())
	}

	return &handlerutil.Out[types.SystemDiskUsage]{
		Body: types.ApiResponse[types.SystemDiskUsage]{
			Success: true,
			Data:    usage,
		},
	}, nil
}

// PruneAll removes unused Docker resources.
func (h *Handler) PruneAll(ctx context.Context, input *PruneAllInput) (*handlerutil.Out[types.PruneAllResult], error) {
	slog.InfoContext(ctx, "System prune operation initiated",
		"containers", input.Body.Containers,
		"images", input.Body.Images,
		"volumes", input.Body.Volumes,
		"networks", input.Body.Networks,
		"buildCache", input.Body.BuildCache)

	runtimeCtx := activitylib.ActivityRuntimeContext(ctx, h.appCtx)
	result := h.service.StartPruneAll(runtimeCtx, input.EnvironmentID, input.Body)

	slog.InfoContext(runtimeCtx, "System prune background activity started", "activityId", result.ActivityID)

	return &handlerutil.Out[types.PruneAllResult]{
		Body: types.ApiResponse[types.PruneAllResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}
