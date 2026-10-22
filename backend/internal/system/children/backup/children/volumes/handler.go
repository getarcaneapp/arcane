package volumes

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Handler serves the system-managed volume backup endpoints.
type Handler struct {
	service *Service
	appCtx  context.Context
}

func NewHandler(service *Service, appCtx context.Context) *Handler {
	return &Handler{service: service, appCtx: appCtx}
}

type SystemVolumeBackupConfigOutput struct {
	Body types.SystemVolumeBackupPolicyCollection
}

type UpdateSystemVolumeBackupConfigInput struct {
	Body types.UpdateSystemVolumeBackupPolicies
}

type SystemVolumeBackupOptionsOutput struct {
	Body []types.SystemVolumeBackupOption
}

type SystemVolumeBackupRunOutput struct {
	Body types.BackupRunAccepted
}

type RunSystemVolumeBackupsInput struct {
	Body *types.RunSystemVolumeBackupsRequest `json:"body,omitempty"`
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
	result, err := h.service.Start(utils.ActivityRuntimeContext(ctx, h.appCtx), *user, kit.FromPtr(input.Body))
	if err != nil {
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
	}
	return &SystemVolumeBackupRunOutput{Body: *result}, nil
}
