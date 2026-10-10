package upgrade

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler serves the self-upgrade and update-all endpoints.
type Handler struct {
	service *Service
	cfg     *config.Config
	appCtx  context.Context
}

func NewHandler(service *Service, cfg *config.Config, appCtx context.Context) *Handler {
	return &Handler{service: service, cfg: cfg, appCtx: appCtx}
}

type CheckUpgradeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// UpgradeCheckResultData is the response for upgrade check.
type UpgradeCheckResultData struct {
	CanUpgrade bool   `json:"canUpgrade"`
	Error      bool   `json:"error"`
	Message    string `json:"message"`
}

type CheckUpgradeOutput struct {
	Body UpgradeCheckResultData
}

type TriggerUpgradeInput struct {
	EnvironmentID string              `path:"id" doc:"Environment ID"`
	Body          *TriggerUpgradeBody `doc:"Optional upgrade parameters"`
}

type TriggerUpgradeBody struct {
	TargetVersion string `json:"targetVersion,omitempty" doc:"Release version to upgrade to; overrides this instance's own version check"`
}

// TriggerUpgradeData reports the upgrade was accepted. UpToDate lets a client skip waiting
// for a restart: an environment already on the newest image is not recreated.
type TriggerUpgradeData struct {
	Message  string `json:"message" doc:"Response message"`
	UpToDate bool   `json:"upToDate" doc:"Environment already runs the newest image, so no restart is expected"`
}

type TriggerUpdateAllInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type UpdateAllStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// CheckUpgradeAvailable checks if a system upgrade is available.
func (h *Handler) CheckUpgradeAvailable(ctx context.Context, input *CheckUpgradeInput) (*CheckUpgradeOutput, error) {
	canUpgrade, err := h.service.CanUpgrade(ctx)
	if err != nil {
		slog.DebugContext(ctx, "System upgrade check failed", "error", err)
		return &CheckUpgradeOutput{
			Body: UpgradeCheckResultData{
				CanUpgrade: false,
				Error:      true,
				Message:    "Failed to check for updates: " + err.Error(),
			},
		}, nil
	}

	return &CheckUpgradeOutput{
		Body: UpgradeCheckResultData{
			CanUpgrade: canUpgrade,
			Error:      false,
			Message:    "System can be upgraded",
		},
	}, nil
}

// TriggerUpgrade triggers a system upgrade.
func (h *Handler) TriggerUpgrade(ctx context.Context, input *TriggerUpgradeInput) (*handlerutil.Out[TriggerUpgradeData], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "System upgrade triggered", "user", user.Username, "userId", user.ID)

	// Resolved before triggering, while the version check still describes the running container.
	upToDate := h.service.AlreadyOnNewestImage(ctx)

	err = h.service.TriggerUpgradeAsync(utils.ActivityRuntimeContext(ctx, h.appCtx), *user, kit.FromPtr(input.Body).TargetVersion)
	if err != nil {
		slog.ErrorContext(ctx, "System upgrade failed", "error", err, "user", user.Username)

		if errors.Is(err, common.ErrUpgradeInProgress) {
			return nil, huma.Error409Conflict("Failed to initiate upgrade: " + err.Error())
		}

		return nil, huma.Error500InternalServerError("Failed to initiate upgrade: " + err.Error())
	}

	message := kit.Ternary(
		upToDate,
		"Already running the newest image. The upgrade pulls it again and leaves the container in place.",
		"Upgrade initiated successfully. A new container is being created and will replace this one shortly.",
	)

	return &handlerutil.Out[TriggerUpgradeData]{
		Body: base.ApiResponse[TriggerUpgradeData]{
			Success: true,
			Data: TriggerUpgradeData{
				Message:  message,
				UpToDate: upToDate,
			},
		},
	}, nil
}

// TriggerUpdateAll starts a fleet-wide update: every remote agent in turn, then the manager last.
func (h *Handler) TriggerUpdateAll(ctx context.Context, input *TriggerUpdateAllInput) (*handlerutil.Out[EnvironmentUpdateJob], error) {
	if h.cfg != nil && h.cfg.AgentMode {
		return nil, huma.Error400BadRequest("update-all is managed on the Arcane manager")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Update-all environments triggered", "user", user.Username, "userId", user.ID)

	job, err := h.service.StartUpdateAll(ctx, *user)
	if err != nil {
		if errors.Is(err, common.ErrUpdateAllInProgress) {
			return nil, huma.Error409Conflict(err.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to initiate upgrade: " + err.Error())
	}

	return &handlerutil.Out[EnvironmentUpdateJob]{
		Body: base.ApiResponse[EnvironmentUpdateJob]{
			Success: true,
			Data:    *job,
		},
	}, nil
}

// GetUpdateAllStatus returns the latest update-all job for live progress polling.
func (h *Handler) GetUpdateAllStatus(ctx context.Context, input *UpdateAllStatusInput) (*handlerutil.Out[EnvironmentUpdateJob], error) {
	if h.cfg != nil && h.cfg.AgentMode {
		return nil, huma.Error400BadRequest("update-all is managed on the Arcane manager")
	}

	job, err := h.service.GetLatestUpdateAllJob(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if job == nil {
		return nil, huma.Error404NotFound("no update-all job found")
	}

	return &handlerutil.Out[EnvironmentUpdateJob]{
		Body: base.ApiResponse[EnvironmentUpdateJob]{
			Success: true,
			Data:    *job,
		},
	}, nil
}
