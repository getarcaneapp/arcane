package handlers

import (
	"context"
	"io"
	"mime/multipart"
	"path"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/types/v2/base"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
)

// buildWorkspaceHandler provides file browsing endpoints for manual build workspaces.
type buildWorkspaceHandler struct {
	service *services.BuildWorkspaceService
}

// RegisterBuildWorkspaces registers build workspace file browser routes.
func RegisterBuildWorkspaces(api huma.API, workspaceService *services.BuildWorkspaceService) {
	h := &buildWorkspaceHandler{service: workspaceService}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse",
		Summary:     "Browse build workspace files",
		Description: "List files and directories under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermBuildWorkspacesManage, h.browseDirectoryInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-content",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse/content",
		Summary:     "Get build workspace file content",
		Description: "Read file content under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermBuildWorkspacesManage, h.getFileContentInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-download",
		Method:      "GET",
		Path:        "/environments/{id}/builds/browse/download",
		Summary:     "Download build workspace file",
		Description: "Download a file from the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermBuildWorkspacesManage, h.downloadFileInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-upload",
		Method:      "POST",
		Path:        "/environments/{id}/builds/browse/upload",
		Summary:     "Upload build workspace file",
		Description: "Upload a file into the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"file": {
								Type:        "string",
								Format:      "binary",
								Description: "File to upload",
							},
						},
						Required: []string{"file"},
					},
				},
			},
		},
	}, authz.PermBuildWorkspacesManage, h.uploadFileInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-mkdir",
		Method:      "POST",
		Path:        "/environments/{id}/builds/browse/mkdir",
		Summary:     "Create build workspace directory",
		Description: "Create a directory under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermBuildWorkspacesManage, h.createDirectoryInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "builds-browse-delete",
		Method:      "DELETE",
		Path:        "/environments/{id}/builds/browse",
		Summary:     "Delete build workspace file",
		Description: "Delete a file or directory under the builds workspace root",
		Tags:        []string{"Builds"},
		Security:    []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}},
	}, authz.PermBuildWorkspacesManage, h.deleteFileInternal)
}

type browseBuildsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" default:"/" doc:"Directory path to browse"`
}

type browseBuildsOutput struct {
	Body base.ApiResponse[[]volumetypes.FileEntry]
}

type getBuildFileContentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File path"`
	MaxBytes      int64  `query:"maxBytes" default:"1048576" doc:"Maximum bytes to read (default 1MB)"`
}

type buildFileContentResponse struct {
	Content  []byte `json:"content"`
	MimeType string `json:"mimeType"`
}

type getBuildFileContentOutput struct {
	Body base.ApiResponse[buildFileContentResponse]
}

type downloadBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File path"`
}

type downloadBuildFileOutput struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	ContentLength      int64  `header:"Content-Length"`
	Body               io.ReadCloser
}

type uploadBuildFileInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	Path          string         `query:"path" default:"/" doc:"Destination path"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type createBuildDirectoryInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"Directory path to create"`
}

type deleteBuildFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Path          string `query:"path" doc:"File or directory path to delete"`
}

func (h *buildWorkspaceHandler) browseDirectoryInternal(ctx context.Context, input *browseBuildsInput) (*browseBuildsOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	entries, err := h.service.ListDirectory(ctx, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &browseBuildsOutput{Body: base.ApiResponse[[]volumetypes.FileEntry]{Success: true, Data: entries}}, nil
}

func (h *buildWorkspaceHandler) getFileContentInternal(ctx context.Context, input *getBuildFileContentInput) (*getBuildFileContentOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	content, mimeType, err := h.service.GetFileContent(ctx, input.Path, input.MaxBytes)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &getBuildFileContentOutput{Body: base.ApiResponse[buildFileContentResponse]{
		Success: true,
		Data:    buildFileContentResponse{Content: content, MimeType: mimeType},
	}}, nil
}

func (h *buildWorkspaceHandler) downloadFileInternal(ctx context.Context, input *downloadBuildFileInput) (*downloadBuildFileOutput, error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	reader, size, err := h.service.DownloadFile(ctx, input.Path)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &downloadBuildFileOutput{
		ContentType:        "application/octet-stream",
		ContentDisposition: "attachment; filename=" + path.Base(input.Path),
		ContentLength:      size,
		Body:               reader,
	}, nil
}

func (h *buildWorkspaceHandler) uploadFileInternal(ctx context.Context, input *uploadBuildFileInput) (*base.ApiResponse[base.MessageResponse], error) {
	if h.service == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	files := input.RawBody.File["file"]
	if len(files) == 0 {
		return nil, huma.Error400BadRequest((&common.NoFileUploadedError{}).Error())
	}

	fileHeader := files[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.FileUploadReadError{Err: err}).Error())
	}
	defer func() { _ = file.Close() }()

	if err := h.service.UploadFile(ctx, input.Path, file, fileHeader.Filename); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    base.MessageResponse{Message: "File uploaded successfully"},
	}, nil
}

func (h *buildWorkspaceHandler) createDirectoryInternal(ctx context.Context, input *createBuildDirectoryInput) (*base.ApiResponse[base.MessageResponse], error) {
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

func (h *buildWorkspaceHandler) deleteFileInternal(ctx context.Context, input *deleteBuildFileInput) (*base.ApiResponse[base.MessageResponse], error) {
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
