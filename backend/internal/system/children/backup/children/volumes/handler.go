package volumes

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	// Handler serves the system-managed volume backup endpoints.
	Handler struct {
		service *Service
		appCtx  context.Context
	}

	SystemVolumeBackupConfigOutput struct {
		Body types.SystemVolumeBackupPolicyCollection
	}

	UpdateSystemVolumeBackupConfigInput struct {
		Body types.UpdateSystemVolumeBackupPolicies
	}

	SystemVolumeBackupOptionsOutput struct {
		Body []types.SystemVolumeBackupOption
	}

	SystemVolumeBackupRunOutput struct {
		Body types.BackupRunAccepted
	}

	RunSystemVolumeBackupsInput struct {
		Body *types.RunSystemVolumeBackupsRequest `json:"body,omitempty"`
	}
)

func NewHandler(service *Service, appCtx context.Context) *Handler {
	return &Handler{service: service, appCtx: appCtx}
}

func (h *Handler) GetSystemVolumeConfig(ctx context.Context, _ *struct{}) (*SystemVolumeBackupConfigOutput, error) {
	config, err := h.service.GetConfig(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemVolumeBackupConfigOutput{Body: *config}, nil
}

func (h *Handler) UpdateSystemVolumeConfig(ctx context.Context, input *UpdateSystemVolumeBackupConfigInput) (*SystemVolumeBackupConfigOutput, error) {
	config, err := h.service.UpdateConfig(ctx, input.Body.Policies)
	if errors.Is(err, common.ErrValidation) {
		return nil, handlerutil.APIError(err)
	}
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &SystemVolumeBackupConfigOutput{Body: *config}, nil
}

func (h *Handler) ListSystemVolumeOptions(ctx context.Context, _ *struct{}) (*SystemVolumeBackupOptionsOutput, error) {
	options, err := h.service.ListOptions(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &SystemVolumeBackupOptionsOutput{Body: options}, nil
}

func (h *Handler) RunSystemVolumeBackups(ctx context.Context, input *RunSystemVolumeBackupsInput) (*SystemVolumeBackupRunOutput, error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.service.Start(activity.ActivityRuntimeContext(ctx, h.appCtx), *user, kit.FromPtr(input.Body))
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &SystemVolumeBackupRunOutput{Body: *result}, nil
}
