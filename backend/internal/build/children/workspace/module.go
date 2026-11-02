package workspace

import (
	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

// Register registers build workspace file browser routes.
func Register(api huma.API, workspaceService *Service, uploadService *upload.UploadService) {
	h := &Handler{service: workspaceService, uploadService: uploadService}

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
		Description: "Copy a complete chunked upload session into the builds workspace root. multipart/form-data bodies " +
			"are still accepted for backward compatibility; that form is deprecated and will be removed in a " +
			"future release.",
		Tags:        []string{"Builds"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: upload.LegacyMultipartMiddleware(api, uploadService, types.UploadKindBuildWorkspace),
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
