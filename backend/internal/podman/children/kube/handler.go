package kube

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	podmantypes "github.com/getarcaneapp/arcane/types/v2/podman"

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

type PlayKubeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          podmantypes.KubePlayRequest
}

type DownKubeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          podmantypes.KubeDownRequest
}

type GenerateKubeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          podmantypes.KubeGenerateRequest
}

func (h *Handler) Play(ctx context.Context, input *PlayKubeInput) (*handlerutil.Out[podmantypes.KubePlayReport], error) {
	report, err := h.service.Play(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to play kube YAML: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "kube.play", "kube", "", map[string]any{"pods": len(report.Pods), "replace": input.Body.Replace})
	return &handlerutil.Out[podmantypes.KubePlayReport]{Body: base.ApiResponse[podmantypes.KubePlayReport]{Success: true, Data: report}}, nil
}

func (h *Handler) Down(ctx context.Context, input *DownKubeInput) (*handlerutil.Out[podmantypes.KubeDownReport], error) {
	report, err := h.service.Down(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to tear down kube YAML: "+apiErr.Message)
	}
	h.audit(ctx, input.EnvironmentID, "kube.down", "kube", "", map[string]any{"pods": len(report.RmReport), "force": input.Body.Force})
	return &handlerutil.Out[podmantypes.KubeDownReport]{Body: base.ApiResponse[podmantypes.KubeDownReport]{Success: true, Data: report}}, nil
}

func (h *Handler) Generate(ctx context.Context, input *GenerateKubeInput) (*handlerutil.Out[podmantypes.KubeGenerateResponse], error) {
	generated, err := h.service.Generate(ctx, input.Body)
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to generate kube YAML: "+apiErr.Message)
	}
	return &handlerutil.Out[podmantypes.KubeGenerateResponse]{Body: base.ApiResponse[podmantypes.KubeGenerateResponse]{Success: true, Data: generated}}, nil
}
