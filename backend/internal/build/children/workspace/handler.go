package workspace

import (
	"context"
	"mime/multipart"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/workspace"
	"getarcane.dev/types/v3"
)

type (
	Handler struct {
		service       *Service
		uploadService *upload.UploadService
	}

	GetBuildWorkspaceInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
	}

	GetBuildWorkspaceFileInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		RelativePath  string `query:"relativePath" doc:"Path relative to the build workspace root"`
	}

	UpdateBuildWorkspaceInput struct {
		RawBody       multipart.Form `contentType:"multipart/form-data"`
		EnvironmentID string         `path:"id" doc:"Environment ID"`
	}

	UploadBuildWorkspaceFileInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		Body          types.WorkspaceUploadRequest
	}
)

func workspaceHTTPError(err error) error {
	apiErr := common.ToAPIError(err)
	return huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
}

func (h *Handler) GetBuildWorkspace(ctx context.Context, _ *GetBuildWorkspaceInput) (*handlerutil.Out[types.Workspace], error) {
	result, err := h.service.Read(ctx)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &handlerutil.Out[types.Workspace]{Body: types.ApiResponse[types.Workspace]{Success: true, Data: *result}}, nil
}

func (h *Handler) GetBuildWorkspaceFile(ctx context.Context, input *GetBuildWorkspaceFileInput) (*handlerutil.Out[types.WorkspaceFileContent], error) {
	result, err := h.service.File(ctx, input.RelativePath)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &handlerutil.Out[types.WorkspaceFileContent]{Body: types.ApiResponse[types.WorkspaceFileContent]{Success: true, Data: *result}}, nil
}

func (h *Handler) DownloadBuildWorkspaceFile(ctx context.Context, input *GetBuildWorkspaceFileInput) (*huma.StreamResponse, error) {
	reader, size, name, err := h.service.Download(ctx, input.RelativePath)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return handlerutil.DownloadResponse(reader, size, name), nil
}

func (h *Handler) UpdateBuildWorkspace(ctx context.Context, input *UpdateBuildWorkspaceInput) (*handlerutil.Out[types.Workspace], error) {
	manifest, err := handlerutil.ParseMultipartJSONPart[types.WorkspaceUpdateManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	permissions, ok := middleware.PermissionsFromContext(ctx)
	if !ok || permissions == nil {
		return nil, huma.Error403Forbidden("insufficient permissions")
	}
	required, valid := authz.BuildWorkspaceRequiredPermissions(manifest.FileChanges)
	if !valid {
		return nil, huma.Error400BadRequest("unsupported build workspace operation")
	}
	for _, permission := range required {
		if !permissions.Allows(permission, input.EnvironmentID) {
			return nil, huma.Error403Forbidden("insufficient permissions for build workspace operation")
		}
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, workspace.MaxFileSizeBytes(h.service.settingsService.GetIntSetting(ctx, "projectWorkspaceMaxFileSizeMb")))
	if err != nil {
		return nil, err
	}
	result, err := h.service.Update(ctx, manifest, uploads)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &handlerutil.Out[types.Workspace]{Body: types.ApiResponse[types.Workspace]{Success: true, Data: *result}}, nil
}

func (h *Handler) UploadBuildWorkspaceFile(ctx context.Context, input *UploadBuildWorkspaceFileInput) (*handlerutil.Out[types.Workspace], error) {
	file, session, cleanup, err := h.uploadService.Consume(ctx, types.UploadKindBuildWorkspace, input.Body.UploadID)
	if err != nil {
		if httpErr := upload.SessionHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	defer cleanup()

	result, err := h.service.Upload(ctx, input.Body.ParentPath, session.Filename, file, session.Size)
	if err != nil {
		return nil, workspaceHTTPError(err)
	}
	return &handlerutil.Out[types.Workspace]{Body: types.ApiResponse[types.Workspace]{Success: true, Data: *result}}, nil
}
