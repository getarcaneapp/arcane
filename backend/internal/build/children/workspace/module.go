package workspace

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func Register(api huma.API, workspaceService *Service, uploadService *upload.UploadService) {
	h := &Handler{
		service:       workspaceService,
		uploadService: uploadService,
	}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-build-workspace",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/builds/workspace",
		Summary:     "Get build workspace",
		Tags:        []string{"Build Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesList, h.GetBuildWorkspace)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-build-workspace-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/builds/workspace/file",
		Summary:     "Get build workspace file",
		Tags:        []string{"Build Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesRead, h.GetBuildWorkspaceFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "download-build-workspace-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/builds/workspace/file/download",
		Summary:     "Download build workspace file",
		Tags:        []string{"Build Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesRead, h.DownloadBuildWorkspaceFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-build-workspace",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/builds/workspace",
		Summary:     "Update build workspace",
		Tags:        []string{"Build Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
		RequestBody: handlerutil.WorkspaceMultipartRequestBody("JSON encoded build workspace manifest"),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.BuildWorkspaceConditions},
	}, authz.PermBuildWorkspacesRead, h.UpdateBuildWorkspace)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "upload-build-workspace-file",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/builds/workspace/upload",
		Summary:     "Upload build workspace file",
		Description: "Write a complete chunked upload session into a build workspace folder",
		Tags:        []string{"Build Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermBuildWorkspacesCreate, h.UploadBuildWorkspaceFile)
}
