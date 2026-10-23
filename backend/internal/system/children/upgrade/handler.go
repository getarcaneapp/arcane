package upgrade

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
	"github.com/getarcaneapp/arcane/types/v3"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// Handler serves the self-upgrade and upgrade job endpoints.
type Handler struct {
	service *Service
	cfg     *config.Config
	appCtx  context.Context
}

func NewHandler(service *Service, cfg *config.Config, appCtx context.Context) *Handler {
	return &Handler{service: service, cfg: cfg, appCtx: appCtx}
}

type CheckUpgradeOutput struct {
	Body types.UpgradeCheckResult
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

type StartUpgradeJobInput struct {
	types.EnvironmentInput
	Body *types.StartUpgradeJobRequest
}

// CheckUpgradeAvailable checks if a system upgrade is available.
func (h *Handler) CheckUpgradeAvailable(ctx context.Context, input *types.EnvironmentInput) (*CheckUpgradeOutput, error) {
	canUpgrade, err := h.service.CanUpgrade(ctx)
	if err != nil {
		slog.DebugContext(ctx, "System upgrade check failed", "error", err)
		return &CheckUpgradeOutput{
			Body: types.UpgradeCheckResult{
				CanUpgrade: false,
				Error:      true,
				Message:    "Failed to check for updates: " + err.Error(),
			},
		}, nil
	}

	return &CheckUpgradeOutput{
		Body: types.UpgradeCheckResult{
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
		apiErr := common.ToAPIError(err)
		return nil, huma.NewError(apiErr.HTTPStatus(), "Failed to initiate upgrade: "+err.Error())
	}

	message := kit.Ternary(
		upToDate,
		"Already running the newest image. The upgrade pulls it again and leaves the container in place.",
		"Upgrade initiated successfully. A new container is being created and will replace this one shortly.",
	)

	return &handlerutil.Out[TriggerUpgradeData]{
		Body: types.ApiResponse[TriggerUpgradeData]{
			Success: true,
			Data: TriggerUpgradeData{
				Message:  message,
				UpToDate: upToDate,
			},
		},
	}, nil
}

// StartUpgradeJob starts an upgrade job: requested remote agents in parallel, then the manager last when requested.
func (h *Handler) StartUpgradeJob(ctx context.Context, input *StartUpgradeJobInput) (*handlerutil.Out[types.UpgradeJob], error) {
	if h.cfg.EdgeAgent {
		return nil, huma.Error400BadRequest("upgrade jobs are managed on the Arcane manager")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Upgrade job triggered", "user", user.Username, "userId", user.ID, "environments", kit.FromPtr(input.Body).EnvironmentIDs)

	job, err := h.service.StartUpgradeJob(ctx, *user, kit.FromPtr(input.Body).EnvironmentIDs)
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error500InternalServerError("Failed to initiate upgrade: " + err.Error())
	}

	body, err := handlerutil.MapOneAPIResponse[*UpgradeJob, types.UpgradeJob](job, func(mapErr error) string {
		return "Failed to initiate upgrade: " + mapErr.Error()
	})
	if err != nil {
		return nil, err
	}
	return &handlerutil.Out[types.UpgradeJob]{Body: body}, nil
}

// GetCurrentUpgradeJob returns the latest upgrade job.
func (h *Handler) GetCurrentUpgradeJob(ctx context.Context, input *types.EnvironmentInput) (*handlerutil.Out[types.UpgradeJob], error) {
	if h.cfg.EdgeAgent {
		return nil, huma.Error400BadRequest("upgrade jobs are managed on the Arcane manager")
	}

	job, err := h.service.GetCurrentUpgradeJob(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	if job == nil {
		return nil, huma.Error404NotFound("no upgrade job found")
	}

	body, err := handlerutil.MapOneAPIResponse[*UpgradeJob, types.UpgradeJob](job, func(mapErr error) string {
		return mapErr.Error()
	})
	if err != nil {
		return nil, err
	}
	return &handlerutil.Out[types.UpgradeJob]{Body: body}, nil
}

func (h *Handler) StreamUpgradeProgress(ctx context.Context, input *types.EnvironmentInput, send sse.Sender) {
	h.service.StreamProgress(ctx, func(event types.UpgradeProgressEvent) bool {
		return send.Data(event) == nil
	})
}
