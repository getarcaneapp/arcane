package browser

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service           *Service
		checkVolumeAccess func(ctx context.Context, envID, perm, volumeName string) error
	}

	BackupHasPathInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
		Path          string `query:"path" doc:"Path to check"`
	}

	BackupHasPathResponse struct {
		Exists bool `json:"exists"`
	}

	ListBackupFilesInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
	}

	BrowseBackupFilesInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		BackupID      string `path:"backupId" doc:"Backup ID"`
		Path          string `query:"path" doc:"Folder path relative to the backup root"`
		Search        string `query:"search" doc:"Case-insensitive full-path search"`
		Start         int    `query:"start" default:"0" doc:"Start index for the page"`
		Limit         int    `query:"limit" default:"20" doc:"Requested page size"`
	}
)

func NewHandler(service *Service, checkVolumeAccess func(ctx context.Context, envID, perm, volumeName string) error) *Handler {
	return &Handler{service: service, checkVolumeAccess: checkVolumeAccess}
}

func (h *Handler) requireBackupAccess(ctx context.Context, envID, backupID string) error {
	if h.checkVolumeAccess == nil {
		return nil
	}
	ps, ok := middleware.PermissionsFromContext(ctx)
	if !ok || ps.Allows(authz.PermVolumesRead, envID) {
		return nil
	}
	entry, err := h.service.deps.Run(ctx, backupID)
	if err != nil || entry == nil {
		return huma.Error403Forbidden("permission denied: " + authz.PermVolumesRead)
	}
	return h.checkVolumeAccess(ctx, envID, authz.PermVolumesRead, entry.VolumeName)
}

func (h *Handler) BackupHasPath(ctx context.Context, input *BackupHasPathInput) (*handlerutil.Out[BackupHasPathResponse], error) {
	if err := h.requireBackupAccess(ctx, input.EnvironmentID, input.BackupID); err != nil {
		return nil, err
	}
	if input.Path == "" {
		return nil, huma.Error400BadRequest("path is required")
	}

	exists, err := h.service.BackupHasPath(ctx, input.BackupID, input.Path)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}

	return &handlerutil.Out[BackupHasPathResponse]{
		Body: types.ApiResponse[BackupHasPathResponse]{
			Success: true,
			Data:    BackupHasPathResponse{Exists: exists},
		},
	}, nil
}

func (h *Handler) ListBackupFiles(ctx context.Context, input *ListBackupFilesInput) (*handlerutil.Out[[]string], error) {
	if err := h.requireBackupAccess(ctx, input.EnvironmentID, input.BackupID); err != nil {
		return nil, err
	}
	files, err := h.service.ListBackupFiles(ctx, input.BackupID)
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}

	return &handlerutil.Out[[]string]{
		Body: types.ApiResponse[[]string]{
			Success: true,
			Data:    files,
		},
	}, nil
}

func (h *Handler) BrowseBackupFiles(ctx context.Context, input *BrowseBackupFilesInput) (*handlerutil.Page[types.BackupFileEntry], error) {
	if err := h.requireBackupAccess(ctx, input.EnvironmentID, input.BackupID); err != nil {
		return nil, err
	}
	params := handlerutil.PaginationParams(input.Start, input.Limit, "", "", input.Search)
	items, page, err := h.service.BrowseBackupFiles(ctx, input.BackupID, input.Path, params)
	if errors.Is(err, common.ErrBadRequest) {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err != nil {
		return nil, huma.NewError(common.ToAPIError(err).HTTPStatus(), err.Error())
	}
	return &handlerutil.Page[types.BackupFileEntry]{Body: types.Paginated[types.BackupFileEntry]{
		Success: true, Data: items, Pagination: handlerutil.PaginationResponse(page),
	}}, nil
}
