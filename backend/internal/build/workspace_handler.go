package build

import (
	"context"
	"io"
	"path"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	uploadtypes "github.com/getarcaneapp/arcane/types/v2/upload"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
)

// BuildWorkspaceHandler provides file browsing endpoints for manual build workspaces.
type BuildWorkspaceHandler struct {
	service       *BuildWorkspaceService
	uploadService *upload.UploadService
}

// RegisterBuildWorkspaces registers build workspace file browser routes.
func RegisterBuildWorkspaces(api huma.API, workspaceService *BuildWorkspaceService, uploadService *upload.UploadService) {
	h := &BuildWorkspaceHandler{service: workspaceService, uploadService: uploadService}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse",
		Summary:     "Browse build workspace files",
		Description: "List files and directories under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesManage, h.BrowseDirectory)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-content",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse/content",
		Summary:     "Get build workspace file content",
		Description: "Read file content under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesManage, h.GetFileContent)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-download",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse/download",
		Summary:     "Download build workspace file",
		Description: "Download a file from the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesManage, h.DownloadFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-upload",
		Method:      "POST",
		Path:        "/environments/{id}/builds/browse/upload",
		Summary:     "Upload build workspace file",
		Description: "Copy a complete chunked upload session into the builds workspace root. multipart/form-data bodies are still accepted for backward compatibility; that form is deprecated and will be removed in a future release.",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: upload.LegacyMultipartMiddleware(api, uploadService, uploadtypes.KindBuildWorkspace),
	}, authz.PermBuildWorkspacesManage, h.UploadFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-mkdir",
		Method:      "POST",
		Path:        "/environments/{id}/builds/browse/mkdir",
		Summary:     "Create build workspace directory",
		Description: "Create a directory under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesManage, h.CreateDirectory)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-delete",
		Method:      "DELETE",
		Path:        "/environments/{id}/builds/browse",
		Summary:     "Delete build workspace file",
		Description: "Delete a file or directory under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesManage, h.DeleteFile)
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

type DownloadBuildFileOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	ContentLength      int64  `header:"Content-Length"`
	Body               io.ReadCloser
}

type UploadBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" default:"/" doc:"Destination path"`
	Body          uploadtypes.ConsumeRequest
}

type CreateBuildDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"Directory path to create"`
}

type DeleteBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File or directory path to delete"`
}

func (h *BuildWorkspaceHandler) BrowseDirectory(ctx context.Context, input *BrowseBuildsInput) (*handlerutil.Out[[]workspacetypes.FileEntry], error) {
	entries, err := h.service.ListDirectory(ctx, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[[]workspacetypes.FileEntry]{Body: base.ApiResponse[[]workspacetypes.FileEntry]{Success: true, Data: entries}}, nil
}

func (h *BuildWorkspaceHandler) GetFileContent(ctx context.Context, input *GetBuildFileContentInput) (*handlerutil.Out[BuildFileContentResponse], error) {
	content, mimeType, err := h.service.GetFileContent(ctx, input.Path, input.MaxBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[BuildFileContentResponse]{Body: base.ApiResponse[BuildFileContentResponse]{
		Success: true,
		Data:    BuildFileContentResponse{Content: content, MimeType: mimeType},
	}}, nil
}

func (h *BuildWorkspaceHandler) DownloadFile(ctx context.Context, input *DownloadBuildFileInput) (*DownloadBuildFileOutput, error) {
	reader, size, err := h.service.DownloadFile(ctx, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &DownloadBuildFileOutput{
		ContentType:        "application/octet-stream",
		ContentDisposition: "attachment; filename=" + path.Base(input.Path),
		ContentLength:      size,
		Body:               reader,
	}, nil
}

func (h *BuildWorkspaceHandler) UploadFile(ctx context.Context, input *UploadBuildFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	file, session, cleanup, err := h.uploadService.Consume(ctx, uploadtypes.KindBuildWorkspace, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	defer cleanup()

	if err := h.service.UploadFile(ctx, input.Path, file, session.Filename, session.Size); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "File uploaded successfully"},
	}, nil
}

func (h *BuildWorkspaceHandler) CreateDirectory(ctx context.Context, input *CreateBuildDirectoryInput) (*base.ApiResponse[base.MessageResponse], error) {
	if err := h.service.CreateDirectory(ctx, input.Path); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "Directory created successfully"},
	}, nil
}

func (h *BuildWorkspaceHandler) DeleteFile(ctx context.Context, input *DeleteBuildFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	if err := h.service.DeleteFile(ctx, input.Path); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "Deleted successfully"},
	}, nil
}
