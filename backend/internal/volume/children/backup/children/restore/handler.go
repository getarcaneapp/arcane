package restore

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/common"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/upload"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Handler struct {
	service         *Service
	activityService *activity.ActivityService
	uploadService   *upload.UploadService
	appCtx          context.Context
}

type RestoreBackupInput struct {
	EnvironmentID string                        `path:"id" doc:"Environment ID"`
	VolumeName    string                        `path:"volumeName" doc:"Volume name"`
	BackupID      string                        `path:"backupId" doc:"Backup ID"`
	Action        string                        `query:"action" required:"true" enum:"restore,restorefiles" doc:"Restore action"`
	Body          *types.BackupRestoreSelection `required:"false"`
}

type UploadAndRestoreInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Body          types.UploadConsumeRequest
}

func NewHandler(service *Service, activityService *activity.ActivityService, uploadService *upload.UploadService, appCtx context.Context) *Handler {
	return &Handler{service: service, activityService: activityService, uploadService: uploadService, appCtx: appCtx}
}

func (h *Handler) RestoreAction(ctx context.Context, input *RestoreBackupInput) (*handlerutil.Out[types.MessageResponse], error) {
	if input.Action == "restorefiles" {
		return h.restoreBackupFiles(ctx, input)
	}
	return h.restoreBackup(ctx, input)
}

func (h *Handler) restoreBackup(ctx context.Context, input *RestoreBackupInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           types.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Restoring backup",
		Message:        "Restoring volume backup",
		SuccessMessage: "Restore initiated successfully",
		Metadata: database.JSON{
			"action":   "restore_volume_backup",
			"backupId": input.BackupID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.service.RestoreBackup(runtimeCtx, input.VolumeName, input.BackupID, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return handlerutil.MessageOutput("Restore initiated successfully", activityID), nil
}

func (h *Handler) restoreBackupFiles(ctx context.Context, input *RestoreBackupInput) (*handlerutil.Out[types.MessageResponse], error) {
	selection := kit.FromPtr(input.Body)
	if len(selection.Paths) == 0 && !selection.SelectAll {
		return nil, huma.Error400BadRequest("restorefiles requires paths or selectAll")
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           types.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Restoring backup files",
		Message:        "Restoring files from volume backup",
		SuccessMessage: "Restore initiated successfully",
		Metadata: database.JSON{
			"action":    "restore_volume_backup_files",
			"backupId":  input.BackupID,
			"paths":     selection.Paths,
			"selectAll": selection.SelectAll,
			"search":    selection.Search,
		},
	}, func(runtimeCtx context.Context) error {
		return h.service.RestoreBackupFiles(runtimeCtx, input.VolumeName, input.BackupID, selection, *user)
	})
	if errors.Is(err, common.ErrBadRequest) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return handlerutil.MessageOutput("Restore initiated successfully", activityID), nil
}

func (h *Handler) UploadAndRestore(ctx context.Context, input *UploadAndRestoreInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	file, session, cleanup, err := h.uploadService.Consume(ctx, types.UploadKindVolumeBackup, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError("Failed to open upload: " + err.Error())
	}
	defer cleanup()

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           types.ActivityTypeResourceAction,
		ResourceType:   "volume",
		ResourceID:     input.VolumeName,
		ResourceName:   input.VolumeName,
		User:           user,
		Step:           "Uploading backup",
		Message:        "Uploading and restoring volume backup",
		SuccessMessage: "Backup uploaded and restored successfully",
		Metadata: database.JSON{
			"action":   "upload_restore_volume_backup",
			"filename": session.Filename,
		},
	}, func(runtimeCtx context.Context) error {
		return h.service.UploadAndRestore(runtimeCtx, input.VolumeName, file, session.Filename, *user)
	})
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return handlerutil.MessageOutput("Backup uploaded and restored successfully", activityID), nil
}
