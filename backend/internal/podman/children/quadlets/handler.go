package quadlets

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

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

type ListQuadletsInput struct {
	EnvironmentID string   `path:"id" doc:"Environment ID"`
	Name          []string `query:"name,explode" doc:"Filter by quadlet name"`
	Pod           []string `query:"pod,explode" doc:"Filter by pod (Podman 6.0+)"`
	Status        []string `query:"status,explode" doc:"Filter by status (Podman 6.0+)"`
	Search        string   `query:"search" doc:"Search query"`
	Sort          string   `query:"sort" doc:"Column to sort by"`
	Order         string   `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int      `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int      `query:"limit" default:"20" doc:"Number of items per page"`
}

type QuadletInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Quadlet file name"`
}

type InstallQuadletInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          podmantypes.QuadletInstallRequest
}

type UpdateQuadletInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Quadlet file name"`
	Body          podmantypes.QuadletUpdateRequest
}

type QuadletActionInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Quadlet file name"`
	Action        string `path:"action" enum:"start,stop,restart" doc:"systemd lifecycle action"`
}

func (h *Handler) List(ctx context.Context, input *ListQuadletsInput) (*handlerutil.Page[podmantypes.QuadletDetail], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.service.Aggregate(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list quadlets: " + err.Error())
		}
		return &handlerutil.Page[podmantypes.QuadletDetail]{Body: body}, nil
	}

	items, page, err := h.service.ListPaginated(ctx, map[string][]string{
		"name":   input.Name,
		"pod":    input.Pod,
		"status": input.Status,
	}, params)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list quadlets: "+apiErr.Message)
	}
	return &handlerutil.Page[podmantypes.QuadletDetail]{
		Body: base.Paginated[podmantypes.QuadletDetail]{
			Success:    true,
			Data:       items,
			Pagination: handlerutil.PaginationResponse(page),
		},
	}, nil
}

func (h *Handler) Get(ctx context.Context, input *QuadletInput) (*handlerutil.Out[podmantypes.QuadletDetail], error) {
	detail, err := h.service.Get(ctx, input.Name)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to get quadlet: "+apiErr.Message)
	}
	return &handlerutil.Out[podmantypes.QuadletDetail]{Body: base.ApiResponse[podmantypes.QuadletDetail]{Success: true, Data: detail}}, nil
}

func (h *Handler) File(ctx context.Context, input *QuadletInput) (*handlerutil.Out[podmantypes.QuadletFile], error) {
	file, err := h.service.File(ctx, input.Name)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to read quadlet file: "+apiErr.Message)
	}
	return &handlerutil.Out[podmantypes.QuadletFile]{Body: base.ApiResponse[podmantypes.QuadletFile]{Success: true, Data: file}}, nil
}

func (h *Handler) Install(ctx context.Context, input *InstallQuadletInput) (*handlerutil.Out[podmantypes.QuadletInstallReport], error) {
	report, err := h.service.Install(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to install quadlet: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "quadlet.install", "quadlet", input.Body.Name, map[string]any{"app": input.Body.App})
	return &handlerutil.Out[podmantypes.QuadletInstallReport]{Body: base.ApiResponse[podmantypes.QuadletInstallReport]{Success: true, Data: report}}, nil
}

func (h *Handler) Update(ctx context.Context, input *UpdateQuadletInput) (*handlerutil.Out[podmantypes.QuadletInstallReport], error) {
	report, err := h.service.Update(ctx, input.Name, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to update quadlet: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "quadlet.update", "quadlet", input.Name, nil)
	return &handlerutil.Out[podmantypes.QuadletInstallReport]{Body: base.ApiResponse[podmantypes.QuadletInstallReport]{Success: true, Data: report}}, nil
}

func (h *Handler) Delete(ctx context.Context, input *QuadletInput) (*handlerutil.Out[podmantypes.QuadletRemoveReport], error) {
	report, err := h.service.Delete(ctx, input.Name)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to remove quadlet: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "quadlet.delete", "quadlet", input.Name, nil)
	return &handlerutil.Out[podmantypes.QuadletRemoveReport]{Body: base.ApiResponse[podmantypes.QuadletRemoveReport]{Success: true, Data: report}}, nil
}

func (h *Handler) Action(ctx context.Context, input *QuadletActionInput) (*handlerutil.Out[podmantypes.UnitStatus], error) {
	status, err := h.service.Lifecycle(ctx, input.Name, map[string]func(podman.Systemd, context.Context, string) error{
		"start":   podman.Systemd.Start,
		"stop":    podman.Systemd.Stop,
		"restart": podman.Systemd.Restart,
	}[input.Action])
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to "+input.Action+" quadlet: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "quadlet."+input.Action, "quadlet", input.Name, map[string]any{"unit": status.Name})
	return &handlerutil.Out[podmantypes.UnitStatus]{Body: base.ApiResponse[podmantypes.UnitStatus]{Success: true, Data: status}}, nil
}
