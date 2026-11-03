package workspace

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

// RegisterRoutes registers the volume workspace endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	basePath := "/environments/{id}/volumes/{volumeName}/workspace"
	tag := "Volume Workspace"
	handlerutil.RegisterSecured(api, handlerutil.Operation("get-volume-workspace", http.MethodGet, basePath, "Get volume workspace", "", tag), authz.PermVolumesRead, h.GetVolumeWorkspace)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"get-volume-workspace-file",
			http.MethodGet,
			basePath+"/file",
			"Get volume workspace file",
			"",
			tag,
		),
		authz.PermVolumesRead,
		h.GetVolumeWorkspaceFile,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"download-volume-workspace-file",
			http.MethodGet,
			basePath+"/file/download",
			"Download volume workspace file",
			"",
			tag,
		),
		authz.PermVolumesRead,
		h.DownloadVolumeWorkspaceFile,
	)
	handlerutil.RegisterSecured(api, huma.Operation{
		OperationID: "update-volume-workspace",
		Method:      http.MethodPatch,
		Path:        basePath,
		Summary:     "Update volume workspace",
		Tags:        []string{tag},
		RequestBody: handlerutil.WorkspaceMultipartRequestBody("JSON encoded volume workspace manifest"),
		Metadata:    map[string]any{authz.MetaPermissionConditions: authz.VolumeWorkspaceConditions},
	}, authz.PermVolumesRead, h.UpdateVolumeWorkspace)
}
