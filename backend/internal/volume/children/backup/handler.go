package backup

import (
	"cmp"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/upload"
	activitylib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/userctx"
)

type Handler struct {
	service            *Service
	activityService    *activity.ActivityService
	environmentService *environment.EnvironmentService
	uploadService      *upload.UploadService
	appCtx             context.Context
}

type ListBackupsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	VolumeName    string `path:"volumeName" doc:"Volume name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction"`
	Start         int    `query:"start" default:"0" doc:"Start index"`
	Limit         int    `query:"limit" default:"20" doc:"Limit"`
	Type          string `query:"type" doc:"Management origin filter"`
	IncludeTotal  bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
}

type ListBackupsOutput struct {
	Body types.PaginatedWithWarnings[types.VolumeBackup]
}

type CreateBackupInput struct {
	EnvironmentID string                           `path:"id" doc:"Environment ID"`
	VolumeName    string                           `path:"volumeName" doc:"Volume name"`
	Body          *types.CreateVolumeBackupRequest `json:"body,omitempty"`
}

type DiscoverVolumeBackupsInput struct {
	EnvironmentID string                             `path:"id" doc:"Environment ID"`
	Body          types.DiscoverVolumeBackupsRequest `doc:"Destination to scan"`
}

type DiscoverVolumeBackupsOutput struct {
	Body types.ApiResponse[types.DiscoverVolumeBackupsResponse]
}

type DeleteBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type DownloadBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type UploadBackupInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Body          types.UploadVolumeBackupRequest
}

func NewHandler(
	service *Service,
	activityService *activity.ActivityService,
	environmentService *environment.EnvironmentService,
	uploadService *upload.UploadService,
	appCtx context.Context,
) *Handler {
	return &Handler{service: service, activityService: activityService, environmentService: environmentService, uploadService: uploadService, appCtx: appCtx}
}

func (h *Handler) DownloadBackup(ctx context.Context, input *DownloadBackupInput) (*huma.StreamResponse, error) {
	user, _ := userctx.CurrentUserFromContext(ctx)
	reader, size, err := h.service.DownloadBackup(ctx, input.BackupID, user)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			defer func() { _ = reader.Close() }()

			humaCtx.SetHeader("Content-Type", "application/x-gzip")
			humaCtx.SetHeader("Content-Disposition", "attachment; filename="+input.BackupID+".tar.gz")
			humaCtx.SetHeader("Content-Length", strconv.FormatInt(size, 10))

			writer := humaCtx.BodyWriter()
			_, _ = io.Copy(writer, reader)
		},
	}, nil
}

func (h *Handler) ListBackups(ctx context.Context, input *ListBackupsInput) (*ListBackupsOutput, error) {
	params := pagination.QueryParams{
		Search:    input.Search,
		Sort:      input.Sort,
		Order:     pagination.SortOrder(input.Order),
		Start:     input.Start,
		Limit:     cmp.Or(input.Limit, 20),
		SkipCount: !input.IncludeTotal,
		Filters: map[string]string{
			"type": input.Type,
		},
	}

	backups, paginationResp, err := h.service.List(ctx, input.VolumeName, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	warning := h.service.backupMountWarning(ctx)

	return &ListBackupsOutput{
		Body: types.PaginatedWithWarnings[types.VolumeBackup]{
			Success:    true,
			Data:       backups,
			Pagination: handlerutil.PaginationResponse(paginationResp),
			Warnings:   kit.Ternary(warning == "", nil, []string{warning}),
		},
	}, nil
}

func (h *Handler) CreateBackup(ctx context.Context, input *CreateBackupInput) (*handlerutil.Out[types.VolumeBackupEntry], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := h.service.StartBackup(utils.ActivityRuntimeContext(ctx, h.appCtx), input.EnvironmentID, input.VolumeName, *user, kit.FromPtr(input.Body))
	if errors.Is(err, h.service.deps.AlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[types.VolumeBackupEntry]{
		Body: types.ApiResponse[types.VolumeBackupEntry]{Success: true, Data: entry},
	}, nil
}

func (h *Handler) DeleteBackup(ctx context.Context, input *DeleteBackupInput) (*handlerutil.Out[types.MessageResponse], error) {
	user, _ := userctx.CurrentUserFromContext(ctx)
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           types.ActivityTypeResourceAction,
		ResourceType:   "volume_backup",
		ResourceID:     input.BackupID,
		ResourceName:   input.BackupID,
		User:           user,
		Step:           "Deleting backup",
		Message:        "Deleting volume backup",
		SuccessMessage: "Backup deleted successfully",
		Metadata: database.JSON{
			"action":   "delete_volume_backup",
			"backupId": input.BackupID,
		},
	}, func(runtimeCtx context.Context) error {
		return h.service.DeleteBackup(runtimeCtx, input.BackupID, user)
	})
	if errors.Is(err, h.service.deps.AlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return handlerutil.MessageOutput("Backup deleted successfully", activityID), nil
}

func (h *Handler) DiscoverBackups(ctx context.Context, input *DiscoverVolumeBackupsInput) (*DiscoverVolumeBackupsOutput, error) {
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Body.S3DestinationID) == "" {
		return nil, huma.Error400BadRequest("select an S3 destination to discover")
	}
	created, failures, err := h.service.DiscoverRemoteBackups(ctx, input.Body.S3DestinationID)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	return &DiscoverVolumeBackupsOutput{
		Body: types.ApiResponse[types.DiscoverVolumeBackupsResponse]{
			Success: true,
			Data:    types.DiscoverVolumeBackupsResponse{Count: created, Errors: failures},
		},
	}, nil
}

func (h *Handler) UploadBackup(ctx context.Context, input *UploadBackupInput) (*handlerutil.Out[*types.VolumeBackup], error) {
	user, _ := userctx.CurrentUserFromContext(ctx)
	var uploaded *types.VolumeBackup
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           types.ActivityTypeResourceAction,
		ResourceType:   "volume_backup",
		ResourceID:     input.BackupID,
		ResourceName:   input.BackupID,
		User:           user,
		Step:           "Uploading backup",
		Message:        "Uploading volume backup to S3",
		SuccessMessage: "Volume backup uploaded successfully",
		Metadata: database.JSON{
			"action":          "upload_volume_backup",
			"backupId":        input.BackupID,
			"s3DestinationId": input.Body.S3DestinationID,
		},
	}, func(runtimeCtx context.Context) error {
		var uploadErr error
		uploaded, uploadErr = h.service.UploadBackup(runtimeCtx, input.BackupID, input.Body.S3DestinationID)
		return uploadErr
	})
	if errors.Is(err, h.service.deps.AlreadyRunning) {
		return nil, huma.Error409Conflict(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	uploaded.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[*types.VolumeBackup]{Body: types.ApiResponse[*types.VolumeBackup]{Success: true, Data: uploaded}}, nil
}
