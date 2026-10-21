package workspace

import (
	"context"
	"path"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// Handler provides file browsing endpoints for manual build workspaces.
type Handler struct {
	service       *Service
	uploadService *upload.UploadService
}

type BrowseBuildsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" default:"/" doc:"Directory path to browse"`
}

type GetBuildFileContentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File path"`
	MaxBytes      int64  `query:"maxBytes" default:"1048576" doc:"Maximum bytes to read (default 1MB)"`
}

type BuildFileContentResponse struct {
	Content  []byte `json:"content"`
	MimeType string `json:"mimeType"`
}

type DownloadBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File path"`
}

type UploadBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" default:"/" doc:"Destination path"`
	Body          types.UploadConsumeRequest
}

type CreateBuildDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"Directory path to create"`
}

type DeleteBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File or directory path to delete"`
}

func workspaceHTTPError(err error) error {
	apiErr := common.ToAPIError(err)
	return huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
}

func (h *Handler) BrowseDirectory(ctx context.Context, input *BrowseBuildsInput) (*handlerutil.Out[[]types.WorkspaceFileEntry], error) {
	entries, err := h.service.ListDirectory(ctx, input.Path)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &handlerutil.Out[[]types.WorkspaceFileEntry]{Body: types.ApiResponse[[]types.WorkspaceFileEntry]{Success: true, Data: entries}}, nil
}

func (h *Handler) GetFileContent(ctx context.Context, input *GetBuildFileContentInput) (*handlerutil.Out[BuildFileContentResponse], error) {
	content, mimeType, err := h.service.GetFileContent(ctx, input.Path, input.MaxBytes)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &handlerutil.Out[BuildFileContentResponse]{Body: types.ApiResponse[BuildFileContentResponse]{
		Success: true,
		Data:    BuildFileContentResponse{Content: content, MimeType: mimeType},
	}}, nil
}

func (h *Handler) DownloadFile(ctx context.Context, input *DownloadBuildFileInput) (*huma.StreamResponse, error) {
	reader, size, err := h.service.DownloadFile(ctx, input.Path)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return handlerutil.DownloadResponse(reader, size, path.Base(input.Path)), nil
}

func (h *Handler) UploadFile(ctx context.Context, input *UploadBuildFileInput) (*types.ApiResponse[types.MessageResponse], error) {
	file, session, cleanup, err := h.uploadService.Consume(ctx, types.UploadKindBuildWorkspace, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	defer cleanup()

	if uploadFileErr := h.service.UploadFile(ctx, input.Path, file, session.Filename, session.Size); uploadFileErr != nil {
		return nil, workspaceHTTPError(uploadFileErr)
	}
	return &types.ApiResponse[types.MessageResponse]{
		Success: true,
		Data:    types.MessageResponse{Message: "File uploaded successfully"},
	}, nil
}

func (h *Handler) CreateDirectory(ctx context.Context, input *CreateBuildDirectoryInput) (*types.ApiResponse[types.MessageResponse], error) {
	if err := h.service.CreateDirectory(ctx, input.Path); err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &types.ApiResponse[types.MessageResponse]{
		Success: true,
		Data:    types.MessageResponse{Message: "Directory created successfully"},
	}, nil
}

func (h *Handler) DeleteFile(ctx context.Context, input *DeleteBuildFileInput) (*types.ApiResponse[types.MessageResponse], error) {
	if err := h.service.DeleteFile(ctx, input.Path); err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &types.ApiResponse[types.MessageResponse]{
		Success: true,
		Data:    types.MessageResponse{Message: "Deleted successfully"},
	}, nil
}
