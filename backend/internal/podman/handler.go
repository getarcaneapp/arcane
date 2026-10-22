package podman

import (
	"cmp"
	"context"
	"log/slog"
	"maps"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Handler struct {
	service      *Service
	eventService *event.EventService
}

type RunPodmanAutoUpdateInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          *types.PodmanAutoUpdateOptions
}

func (h *Handler) GetStatus(ctx context.Context, _ *types.EnvironmentInput) (*handlerutil.Out[types.PodmanStatus], error) {
	return &handlerutil.Out[types.PodmanStatus]{Body: types.ApiResponse[types.PodmanStatus]{Success: true, Data: h.service.Status(ctx)}}, nil
}

func (h *Handler) GetInfo(ctx context.Context, _ *types.EnvironmentInput) (*handlerutil.Out[types.PodmanInfo], error) {
	info, err := h.service.Info(ctx)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to get Podman info: "+apiErr.Message)
	}
	return &handlerutil.Out[types.PodmanInfo]{Body: types.ApiResponse[types.PodmanInfo]{Success: true, Data: info}}, nil
}

func (h *Handler) RunAutoUpdate(ctx context.Context, input *RunPodmanAutoUpdateInput) (*handlerutil.Out[types.PodmanAutoUpdateResult], error) {
	opts := kit.FromPtr(input.Body)
	result, err := h.service.AutoUpdate(ctx, opts)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to run Podman auto-update: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "autoupdate", "autoupdate", "", map[string]any{
		"mode":    result.Mode,
		"dryRun":  opts.DryRun,
		"reports": len(result.Reports),
	})
	return &handlerutil.Out[types.PodmanAutoUpdateResult]{Body: types.ApiResponse[types.PodmanAutoUpdateResult]{Success: true, Data: result}}, nil
}

func (h *Handler) audit(ctx context.Context, environmentID, action, resourceType, resourceName string, metadata map[string]any) {
	actor := handlerutil.CurrentActor(ctx)
	meta := database.JSON{"action": action}
	maps.Copy(meta, metadata)

	if _, err := h.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventType("podman." + action),
		Severity:      event.EventSeverityInfo,
		Title:         "Podman operation: " + action,
		Description:   "Podman operation '" + action + "' completed",
		ResourceType:  new("podman." + resourceType),
		ResourceID:    mo.EmptyableToOption(resourceName).ToPointer(),
		ResourceName:  mo.EmptyableToOption(resourceName).ToPointer(),
		UserID:        mo.EmptyableToOption(actor.ID).ToPointer(),
		Username:      mo.EmptyableToOption(actor.Username).ToPointer(),
		EnvironmentID: new(cmp.Or(environmentID, "0")),
		Metadata:      meta,
	}); err != nil {
		slog.WarnContext(ctx, "Failed to audit Podman mutation", "action", action, "error", err)
	}
}
