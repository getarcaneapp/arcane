package backup

import (
	"cmp"
	"context"
	"io"
	"mime"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/samber/mo"

	kit "kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/upload"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/pagination"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service            *Service
		activityService    *activity.ActivityService
		environmentService *environment.EnvironmentService
		uploadService      *upload.UploadService
		appCtx             context.Context
		checkVolumeAccess  func(ctx context.Context, envID, perm, volumeName string) error
	}

	ListBackupsInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		VolumeName    string `path:"volumeName" doc:"Volume name"`
		Search        string `query:"search" doc:"Search query"`
		Sort          string `query:"sort" doc:"Column to sort by"`
		Order         string `query:"order" default:"asc" doc:"Sort direction"`
		Type          string `query:"type" doc:"Management origin filter"`
		Start         int    `query:"start" default:"0" doc:"Start index"`
		Limit         int    `query:"limit" default:"20" doc:"Limit"`
		IncludeTotal  bool   `query:"includeTotal" doc:"Return exact totalItems and totalPages; otherwise they are -1 and hasNextPage reports whether more rows exist"`
	}

	ListBackupsOutput struct {
		Body types.PaginatedWithWarnings[types.VolumeBackup]
	}

	CreateBackupInput struct {
		Body          *types.CreateVolumeBackupRequest `json:"body,omitempty"`
		EnvironmentID string                           `path:"id" doc:"Environment ID"`
		VolumeName    string                           `path:"volumeName" doc:"Volume name"`
	}

	DiscoverVolumeBackupsInput struct {
		EnvironmentID string                       `path:"id" doc:"Environment ID"`
		Body          types.DiscoverBackupsRequest `doc:"Destination to scan"`
	}

	DiscoverVolumeBackupsOutput struct {
		Body types.ApiResponse[types.DiscoverVolumeBackupsResponse]
	}

	DeleteBackupInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
	}

	DownloadBackupInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
	}

	ExportVolumeArchiveInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		VolumeName    string `path:"volumeName" doc:"Volume name"`
	}

	UploadBackupInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
		Body          types.UploadVolumeBackupRequest
	}

	HandlerParams struct {
		Service            *Service
		ActivityService    *activity.ActivityService
		EnvironmentService *environment.EnvironmentService
		UploadService      *upload.UploadService
		AppCtx             context.Context
		CheckVolumeAccess  func(ctx context.Context, envID, perm, volumeName string) error
	}
)

func NewHandler(p HandlerParams) *Handler {
	return &Handler{
		service:            p.Service,
		activityService:    p.ActivityService,
		environmentService: p.EnvironmentService,
		uploadService:      p.UploadService,
		appCtx:             p.AppCtx,
		checkVolumeAccess:  p.CheckVolumeAccess,
	}
}

func (h *Handler) requireBackupAccess(ctx context.Context, envID, perm, backupID string) error {
	if h.checkVolumeAccess == nil {
		return nil
	}
	ps, ok := middleware.PermissionsFromContext(ctx)
	if !ok || ps.Allows(perm, envID) {
		return nil
	}
	entry, err := h.service.deps.Store.Run(ctx, backupID)
	if err != nil || entry == nil {
		return huma.Error403Forbidden("permission denied: " + perm)
	}
	return h.checkVolumeAccess(ctx, envID, perm, entry.VolumeName)
}

func (h *Handler) DownloadBackup(ctx context.Context, input *DownloadBackupInput) (*huma.StreamResponse, error) {
	if err := h.requireBackupAccess(ctx, input.EnvironmentID, authz.PermVolumesRead, input.BackupID); err != nil {
		return nil, err
	}
	user, _ := handlerutil.CurrentUserFromContext(ctx)
	reader, size, err := h.service.DownloadBackup(ctx, input.BackupID, user)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			defer func() { _ = reader.Close() }()

			humaCtx.SetHeader("Content-Type", "application/x-gzip")
			humaCtx.SetHeader("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": input.BackupID + ".tar.gz"}))
			humaCtx.SetHeader("Content-Length", strconv.FormatInt(size, 10))

			writer := humaCtx.BodyWriter()
			_, _ = io.Copy(writer, reader)
		},
	}, nil
}

func (h *Handler) ListBackups(ctx context.Context, input *ListBackupsInput) (*ListBackupsOutput, error) {
	if h.checkVolumeAccess != nil {
		if err := h.checkVolumeAccess(ctx, input.EnvironmentID, authz.PermVolumesRead, input.VolumeName); err != nil {
			return nil, err
		}
	}
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
	if h.checkVolumeAccess != nil {
		if err := h.checkVolumeAccess(ctx, input.EnvironmentID, authz.PermVolumesBackup, input.VolumeName); err != nil {
			return nil, err
		}
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	entry, err := h.service.StartBackup(activitylib.ActivityRuntimeContext(ctx, h.appCtx), input.EnvironmentID, input.VolumeName, *user, kit.FromPtr(input.Body))
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.VolumeBackupEntry]{
		Body: types.ApiResponse[types.VolumeBackupEntry]{Success: true, Data: entry},
	}, nil
}

func (h *Handler) DeleteBackup(ctx context.Context, input *DeleteBackupInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.requireBackupAccess(ctx, input.EnvironmentID, authz.PermVolumesBackup, input.BackupID); err != nil {
		return nil, err
	}
	user, _ := handlerutil.CurrentUserFromContext(ctx)
	runtimeCtx := activitylib.ActivityRuntimeContext(ctx, h.appCtx)
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
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return handlerutil.MessageOutput("Backup deleted successfully", activityID), nil
}

func (h *Handler) DiscoverBackups(ctx context.Context, input *DiscoverVolumeBackupsInput) (*DiscoverVolumeBackupsOutput, error) {
	if ps, ok := middleware.PermissionsFromContext(ctx); ok && !ps.Allows(authz.PermVolumesBackup, input.EnvironmentID) {
		return nil, huma.Error403Forbidden("permission denied: " + authz.PermVolumesBackup)
	}
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}
	if err := input.Body.Validate(); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	created, failures, err := h.service.DiscoverBackups(ctx, input.Body)
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
	if err := h.requireBackupAccess(ctx, input.EnvironmentID, authz.PermVolumesBackup, input.BackupID); err != nil {
		return nil, err
	}
	user, _ := handlerutil.CurrentUserFromContext(ctx)
	var uploaded *types.VolumeBackup
	runtimeCtx := activitylib.ActivityRuntimeContext(ctx, h.appCtx)
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
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	uploaded.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[*types.VolumeBackup]{Body: types.ApiResponse[*types.VolumeBackup]{Success: true, Data: uploaded}}, nil
}

func (h *Handler) ExportVolumeArchive(ctx context.Context, input *ExportVolumeArchiveInput) (*handlerutil.Out[types.UploadSession], error) {
	if h.checkVolumeAccess != nil {
		if err := h.checkVolumeAccess(ctx, input.EnvironmentID, authz.PermVolumesBackup, input.VolumeName); err != nil {
			return nil, err
		}
	}
	if _, err := handlerutil.RequireUser(ctx); err != nil {
		return nil, err
	}
	session, err := h.service.ExportVolumeArchive(ctx, input.VolumeName)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Out[types.UploadSession]{
		Body: types.ApiResponse[types.UploadSession]{
			Success: true,
			Data:    *session,
		},
	}, nil
}
