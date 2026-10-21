package browser

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

type Handler struct {
	service *Service
}

type BackupHasPathInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Path          string `query:"path" doc:"Path to check"`
}

type BackupHasPathResponse struct {
	Exists bool `json:"exists"`
}

type ListBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
}

type BrowseBackupFilesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	BackupID      string `path:"backupId" doc:"Backup ID"`
	Path          string `query:"path" doc:"Folder path relative to the backup root"`
	Search        string `query:"search" doc:"Case-insensitive full-path search"`
	Start         int    `query:"start" default:"0" doc:"Start index for the page"`
	Limit         int    `query:"limit" default:"20" doc:"Requested page size"`
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) BackupHasPath(ctx context.Context, input *BackupHasPathInput) (*handlerutil.Out[BackupHasPathResponse], error) {
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
