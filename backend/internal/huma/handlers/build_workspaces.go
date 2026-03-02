package handlers

import (
	"context"
	"io"
	"path"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/types/base"
	volumetypes "github.com/getarcaneapp/arcane/types/volume"
)

// BuildWorkspaceHandler provides file browsing endpoints for manual build workspaces.
type BuildWorkspaceHandler struct {
	service *services.BuildWorkspaceService
}

// RegisterBuildWorkspaces registers build workspace file browser routes.
func RegisterBuildWorkspaces(api huma.API, workspaceService *services.BuildWorkspaceService) {
	h := &BuildWorkspaceHandler{service: workspaceService}

	huma.Register(api, huma.Operation{
		OperationID: "builds-browse",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse",
		Summary:     "Browse build workspace files",
		Description: "List files and directories under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.BrowseDirectory)

	huma.Register(api, huma.Operation{
		OperationID: "builds-browse-content",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse/content",
		Summary:     "Get build workspace file content",
		Description: "Read file content under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.GetFileContent)

	huma.Register(api, huma.Operation{
		OperationID: "builds-browse-download",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse/download",
		Summary:     "Download build workspace file",
		Description: "Download a file from the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DownloadFile)

	huma.Register(api, huma.Operation{
		OperationID: "builds-browse-upload",
		Method:      "POST",
		Path:        "/environments/{id}/builds/browse/upload",
		Summary:     "Upload build workspace file",
		Description: "Upload a file into the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.UploadFile)

	huma.Register(api, huma.Operation{
		OperationID: "builds-browse-mkdir",
		Method:      "POST",
		Path:        "/environments/{id}/builds/browse/mkdir",
		Summary:     "Create build workspace directory",
		Description: "Create a directory under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.CreateDirectory)

	huma.Register(api, huma.Operation{
		OperationID: "builds-browse-delete",
		Method:      "DELETE",
		Path:        "/environments/{id}/builds/browse",
		Summary:     "Delete build workspace file",
		Description: "Delete a file or directory under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, h.DeleteFile)
}

type BrowseBuildsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" default:"/" doc:"Directory path to browse"`
}

type BrowseBuildsOutput struct {
	Body base.ApiResponse[[]volumetypes.FileEntry]
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

type GetBuildFileContentOutput struct {
	Body base.ApiResponse[BuildFileContentResponse]
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
	EnvironmentID string        `path:"id" doc:"Environment ID"`
	Path          string        `query:"path" default:"/" doc:"Destination path"`
	File          huma.FormFile `form:"file" doc:"File to upload"`
}

type CreateBuildDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"Directory path to create"`
}

type DeleteBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File or directory path to delete"`
}

func (h *BuildWorkspaceHandler) BrowseDirectory(ctx context.Context, input *BrowseBuildsInput) (*BrowseBuildsOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	entries, err := h.service.ListDirectory(ctx, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &BrowseBuildsOutput{Body: base.ApiResponse[[]volumetypes.FileEntry]{Success: true, Data: entries}}, nil
}

func (h *BuildWorkspaceHandler) GetFileContent(ctx context.Context, input *GetBuildFileContentInput) (*GetBuildFileContentOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	content, mimeType, err := h.service.GetFileContent(ctx, input.Path, input.MaxBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &GetBuildFileContentOutput{Body: base.ApiResponse[BuildFileContentResponse]{
		Success: true,
		Data:    BuildFileContentResponse{Content: content, MimeType: mimeType},
	}}, nil
}

func (h *BuildWorkspaceHandler) DownloadFile(ctx context.Context, input *DownloadBuildFileInput) (*DownloadBuildFileOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
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
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.service.UploadFile(ctx, input.Path, input.File, input.File.Filename); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "File uploaded successfully"},
	}, nil
}

func (h *BuildWorkspaceHandler) CreateDirectory(ctx context.Context, input *CreateBuildDirectoryInput) (*base.ApiResponse[base.MessageResponse], error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.service.CreateDirectory(ctx, input.Path); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "Directory created successfully"},
	}, nil
}

func (h *BuildWorkspaceHandler) DeleteFile(ctx context.Context, input *DeleteBuildFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := h.service.DeleteFile(ctx, input.Path); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "Deleted successfully"},
	}, nil
}
