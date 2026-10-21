package pods

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
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

type ListPodsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetPodInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	PodID         string `path:"podId" doc:"Pod name or ID"`
}

type CreatePodInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          types.PodCreateRequest
}

type PodActionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	PodID         string `path:"podId" doc:"Pod name or ID"`
	Action        string `path:"action" enum:"start,stop,restart,pause,unpause,kill" doc:"Lifecycle action"`
	Body          *types.PodActionRequest
}

type DeletePodInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	PodID         string `path:"podId" doc:"Pod name or ID"`
	Force         bool   `query:"force" doc:"Stop and remove running containers"`
}

type PrunePodsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type PodTopInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	PodID         string `path:"podId" doc:"Pod name or ID"`
	PsArgs        string `query:"psArgs" doc:"Arguments passed to ps"`
}

type PodStatsInput struct {
	EnvironmentID string   `path:"id" doc:"Environment ID"`
	Names         []string `query:"names" doc:"Pod names or IDs; empty reports every pod"`
}

func (h *Handler) List(ctx context.Context, input *ListPodsInput) (*handlerutil.Page[types.Pod], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list pods: " + err.Error())
		}
		return &handlerutil.Page[types.Pod]{Body: body}, nil
	}

	items, page, err := h.service.ListPaginated(ctx, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list pods: "+apiErr.Message)
	}
	return &handlerutil.Page[types.Pod]{
		Body: types.Paginated[types.Pod]{
			Success:    true,
			Data:       kit.Ternary(items != nil, items, []types.Pod{}),
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *Handler) Get(ctx context.Context, input *GetPodInput) (*handlerutil.Out[types.PodInspect], error) {
	pod, err := h.service.Inspect(ctx, input.PodID)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to inspect pod: "+apiErr.Message)
	}
	return &handlerutil.Out[types.PodInspect]{Body: types.ApiResponse[types.PodInspect]{Success: true, Data: pod}}, nil
}

func (h *Handler) Create(ctx context.Context, input *CreatePodInput) (*handlerutil.Out[types.PodCreateResponse], error) {
	id, err := h.service.Create(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to create pod: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "pod.create", "pod", input.Body.Name, map[string]any{"podId": id})
	return &handlerutil.Out[types.PodCreateResponse]{Body: types.ApiResponse[types.PodCreateResponse]{Success: true, Data: types.PodCreateResponse{ID: id}}}, nil
}

func (h *Handler) Action(ctx context.Context, input *PodActionInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.Action(ctx, input.PodID, podman.PodAction(input.Action), kit.FromPtr(input.Body)); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to "+input.Action+" pod: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "pod."+input.Action, "pod", input.PodID, nil)
	return handlerutil.MessageOutput("Pod "+input.Action+" completed", ""), nil
}

func (h *Handler) Delete(ctx context.Context, input *DeletePodInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.Remove(ctx, input.PodID, input.Force); err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to remove pod: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "pod.delete", "pod", input.PodID, map[string]any{"force": input.Force})
	return handlerutil.MessageOutput("Pod removed", ""), nil
}

func (h *Handler) Prune(ctx context.Context, input *PrunePodsInput) (*handlerutil.Out[[]types.PodPruneReport], error) {
	reports, err := h.service.Prune(ctx)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to prune pods: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "pod.prune", "pod", "", map[string]any{"removed": len(reports)})
	return &handlerutil.Out[[]types.PodPruneReport]{
		Body: types.ApiResponse[[]types.PodPruneReport]{
			Success: true,
			Data:    kit.Ternary(reports != nil, reports, []types.PodPruneReport{}),
		},
	}, nil
}

func (h *Handler) Top(ctx context.Context, input *PodTopInput) (*handlerutil.Out[types.PodTop], error) {
	top, err := h.service.Top(ctx, input.PodID, input.PsArgs)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list pod processes: "+apiErr.Message)
	}
	return &handlerutil.Out[types.PodTop]{Body: types.ApiResponse[types.PodTop]{Success: true, Data: top}}, nil
}

func (h *Handler) Stats(ctx context.Context, input *PodStatsInput) (*handlerutil.Out[[]types.PodStats], error) {
	stats, err := h.service.Stats(ctx, input.Names)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to read pod stats: "+apiErr.Message)
	}
	return &handlerutil.Out[[]types.PodStats]{
		Body: types.ApiResponse[[]types.PodStats]{
			Success: true,
			Data:    kit.Ternary(stats != nil, stats, []types.PodStats{}),
		},
	}, nil
}
