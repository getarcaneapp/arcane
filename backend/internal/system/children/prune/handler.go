package prune

import (
	"context"
	"log/slog"

	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/system"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the system prune endpoint.
type Handler struct {
	service *Service
	appCtx  context.Context
}

func NewHandler(service *Service, appCtx context.Context) *Handler {
	return &Handler{service: service, appCtx: appCtx}
}

type PruneAllInput struct {
	EnvironmentID string                 `path:"id" doc:"Environment ID"`
	Body          system.PruneAllRequest `doc:"Prune options"`
}

// PruneAll removes unused Docker resources.
func (h *Handler) PruneAll(ctx context.Context, input *PruneAllInput) (*handlerutil.Out[system.PruneAllResult], error) {
	slog.InfoContext(ctx, "System prune operation initiated",
		"containers", input.Body.Containers,
		"images", input.Body.Images,
		"volumes", input.Body.Volumes,
		"networks", input.Body.Networks,
		"buildCache", input.Body.BuildCache)

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	result := h.service.StartPruneAll(runtimeCtx, input.EnvironmentID, input.Body)

	slog.InfoContext(runtimeCtx, "System prune background activity started", "activityId", result.ActivityID)

	return &handlerutil.Out[system.PruneAllResult]{
		Body: base.ApiResponse[system.PruneAllResult]{
			Success: true,
			Data:    *result,
		},
	}, nil
}
