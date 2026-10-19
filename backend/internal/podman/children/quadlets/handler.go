package quadlets

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/podman"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
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
	Name          []string `query:"name" doc:"Filter by quadlet name"`
	Pod           []string `query:"pod" doc:"Filter by pod (Podman 6.0+)"`
	Status        []string `query:"status" doc:"Filter by status (Podman 6.0+)"`
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

func (h *Handler) List(ctx context.Context, input *ListQuadletsInput) (*handlerutil.Out[[]podmantypes.QuadletDetail], error) {
	items, err := h.service.List(ctx, map[string][]string{
		"name":   input.Name,
		"pod":    input.Pod,
		"status": input.Status,
	})
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to list quadlets: "+apiErr.Message)
	}
	return &handlerutil.Out[[]podmantypes.QuadletDetail]{Body: base.ApiResponse[[]podmantypes.QuadletDetail]{Success: true, Data: items}}, nil
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
