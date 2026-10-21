package workspace

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
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
	updateOperation := handlerutil.Operation("update-volume-workspace", http.MethodPatch, basePath, "Update volume workspace", "", tag)
	updateOperation.RequestBody = handlerutil.WorkspaceMultipartRequestBody("JSON encoded volume workspace manifest")
	handlerutil.RegisterSecured(api, updateOperation, authz.PermVolumesRead, h.UpdateVolumeWorkspace)
}
