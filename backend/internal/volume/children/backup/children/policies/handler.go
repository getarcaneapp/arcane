package policies

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/userctx"
)

type Handler struct {
	service            *Service
	activityService    *activity.ActivityService
	environmentService *environment.EnvironmentService
	appCtx             context.Context
}

type GetVolumeBackupPolicyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
}

type UpdateVolumeBackupPolicyInput struct {
	EnvironmentID string                           `path:"id" doc:"Environment ID"`
	VolumeName    string                           `path:"volumeName" doc:"Volume name"`
	Body          types.UpdateVolumeBackupPolicies `doc:"Scheduled volume backup policies"`
}

func NewHandler(service *Service, activityService *activity.ActivityService, environmentService *environment.EnvironmentService, appCtx context.Context) *Handler {
	return &Handler{service: service, activityService: activityService, environmentService: environmentService, appCtx: appCtx}
}

func (h *Handler) GetBackupPolicy(ctx context.Context, input *GetVolumeBackupPolicyInput) (*handlerutil.Out[types.VolumeBackupPolicyCollection], error) {
	if input.EnvironmentID != "0" {
		if h.environmentService == nil {
			return nil, huma.Error500InternalServerError("environment service not available")
		}
		remotePath := fmt.Sprintf("/api/environments/0/volumes/%s/backup-policy", url.PathEscape(input.VolumeName))
		response, err := handlerutil.RemoteJSONProxy(
			h.environmentService.ProxyJSONRequest,
		).JSON[types.ApiResponse[types.VolumeBackupPolicyCollection]](
			ctx,
			input.EnvironmentID,
			http.MethodGet,
			remotePath,
			nil,
		)
		if err != nil {
			return nil, err
		}
		return &handlerutil.Out[types.VolumeBackupPolicyCollection]{Body: *response}, nil
	}

	policies, err := h.service.GetBackupPolicies(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[types.VolumeBackupPolicyCollection]{Body: types.ApiResponse[types.VolumeBackupPolicyCollection]{Success: true, Data: *policies}}, nil
}

func (h *Handler) UpdateBackupPolicy(ctx context.Context, input *UpdateVolumeBackupPolicyInput) (*handlerutil.Out[types.VolumeBackupPolicyCollection], error) {
	user, _ := userctx.CurrentUserFromContext(ctx)
	var policies *types.VolumeBackupPolicyCollection
	var remoteResponse *types.ApiResponse[types.VolumeBackupPolicyCollection]
	errorStatus := http.StatusBadRequest
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	_, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           types.ActivityTypeResourceAction,
		ResourceType:   "volume_backup_policy",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Saving backup policies",
		Message:        "Saving volume backup policies",
		SuccessMessage: "Volume backup policies saved successfully",
		Metadata: database.JSON{
			"action":      "update_volume_backup_policy",
			"policyCount": len(input.Body.Policies),
		},
	}, func(activityCtx context.Context) error {
		if input.EnvironmentID != "0" {
			if h.environmentService == nil {
				errorStatus = http.StatusInternalServerError
				return errors.New("environment service not available")
			}
			hasS3 := false
			for _, policy := range input.Body.Policies {
				if policy.S3Enabled {
					hasS3 = true
					break
				}
			}
			// Destinations are reconciled before every policy write so the agent
			// can validate S3 references. Only batches that actually need S3
			// fail on a sync error; local-only edits proceed.
			h.environmentService.ForgetSyncState(input.EnvironmentID)
			if syncErr := h.environmentService.SyncS3DestinationsToEnvironment(activityCtx, input.EnvironmentID); syncErr != nil {
				if hasS3 {
					errorStatus = http.StatusBadGateway
					return fmt.Errorf("failed to synchronize S3 destinations to environment: %w", syncErr)
				}
				slog.WarnContext(activityCtx, "S3 destination sync failed before local-only volume backup policy write", "environmentId", input.EnvironmentID, "error", syncErr)
			}
			remotePath := fmt.Sprintf("/api/environments/0/volumes/%s/backup-policy", url.PathEscape(input.VolumeName))
			var proxyErr error
			remoteResponse, proxyErr = handlerutil.RemoteJSONProxy(
				h.environmentService.ProxyJSONRequest,
			).JSON[types.ApiResponse[types.VolumeBackupPolicyCollection]](
				activityCtx,
				input.EnvironmentID,
				http.MethodPut,
				remotePath,
				input.Body,
			)
			if proxyErr != nil {
				errorStatus = http.StatusBadGateway
				if statusErr, ok := errors.AsType[huma.StatusError](proxyErr); ok && statusErr.GetStatus() == http.StatusNotFound {
					errorStatus = http.StatusNotFound
				}
			}
			return proxyErr
		}
		var updateErr error
		policies, updateErr = h.service.UpdateBackupPolicies(activityCtx, input.VolumeName, input.Body.Policies)
		return updateErr
	})
	if err != nil {
		if errorStatus == http.StatusInternalServerError {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if errorStatus == http.StatusBadGateway || errorStatus == http.StatusNotFound {
			return nil, huma.NewError(errorStatus, err.Error())
		}
		return nil, huma.Error400BadRequest(err.Error())
	}
	if remoteResponse != nil {
		return &handlerutil.Out[types.VolumeBackupPolicyCollection]{Body: *remoteResponse}, nil
	}
	return &handlerutil.Out[types.VolumeBackupPolicyCollection]{Body: types.ApiResponse[types.VolumeBackupPolicyCollection]{Success: true, Data: *policies}}, nil
}
