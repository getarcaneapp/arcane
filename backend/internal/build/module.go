package build

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/build/children/workspace"
	"github.com/getarcaneapp/arcane/backend/v3/internal/upload"
)

// RegisterBuildWorkspaces registers the build workspace file browser routes.
func RegisterBuildWorkspaces(api huma.API, buildService *BuildService, uploadService *upload.UploadService) {
	var workspaceService *workspace.Service
	if buildService != nil {
		workspaceService = buildService.workspace
	}
	workspace.Register(api, workspaceService, uploadService)
}
