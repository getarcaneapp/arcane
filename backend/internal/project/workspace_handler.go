package project

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	workspacepkg "github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
	"github.com/getarcaneapp/arcane/types/v2/base"
	projecttypes "github.com/getarcaneapp/arcane/types/v2/project"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/samber/mo"
)

type GetProjectWorkspaceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type GetProjectWorkspaceFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	RelativePath  string `query:"relativePath" doc:"Path relative to the project workspace root"`
}

type UpdateProjectWorkspaceInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	ProjectID     string         `path:"projectId" doc:"Project ID"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

func registerProjectWorkspaceRoutesInternal(api huma.API, h *ProjectHandler) {
	basePath := "/environments/{id}/projects/{projectId}/workspace"
	tag := "Project Workspace"
	handlerutil.RegisterSecured(api, handlerutil.Operation("get-project-workspace", http.MethodGet, basePath, "Get project workspace", "", tag), authz.PermProjectsRead, h.GetProjectWorkspace)
	handlerutil.RegisterSecured(api, handlerutil.Operation("get-project-workspace-file", http.MethodGet, basePath+"/file", "Get project workspace file", "", tag), authz.PermProjectsRead, h.GetProjectWorkspaceFile)
	handlerutil.RegisterSecured(api, handlerutil.Operation("download-project-workspace-file", http.MethodGet, basePath+"/file/download", "Download project workspace file", "", tag), authz.PermProjectsRead, h.DownloadProjectWorkspaceFile)
	updateOperation := handlerutil.Operation("update-project-workspace", http.MethodPut, basePath, "Update project workspace", "", tag)
	updateOperation.RequestBody = handlerutil.WorkspaceMultipartRequestBody("JSON encoded project workspace manifest")
	handlerutil.RegisterSecured(api, updateOperation, authz.PermProjectsUpdate, h.UpdateProjectWorkspace)
}

func projectWorkspaceHTTPErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrProjectWorkspaceConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrProjectArchived):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrProjectWorkspaceForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, common.ErrProjectWorkspaceNotFound), errors.Is(err, common.ErrProjectNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrProjectWorkspaceBadRequest):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError("internal error")
	}
}

func (h *ProjectHandler) GetProjectWorkspace(ctx context.Context, input *GetProjectWorkspaceInput) (*handlerutil.Out[workspacetypes.Workspace], error) {
	result, err := h.projectService.GetProjectWorkspace(ctx, input.ProjectID)
	if err != nil {
		return nil, projectWorkspaceHTTPErrorInternal(err)
	}
	return &handlerutil.Out[workspacetypes.Workspace]{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: *result}}, nil
}

func (h *ProjectHandler) GetProjectWorkspaceFile(ctx context.Context, input *GetProjectWorkspaceFileInput) (*handlerutil.Out[workspacetypes.FileContent], error) {
	result, err := h.projectService.GetProjectWorkspaceFile(ctx, input.ProjectID, input.RelativePath)
	if err != nil {
		return nil, projectWorkspaceHTTPErrorInternal(err)
	}
	return &handlerutil.Out[workspacetypes.FileContent]{Body: base.ApiResponse[workspacetypes.FileContent]{Success: true, Data: *result}}, nil
}

func (h *ProjectHandler) DownloadProjectWorkspaceFile(ctx context.Context, input *GetProjectWorkspaceFileInput) (*huma.StreamResponse, error) {
	reader, size, name, err := h.projectService.DownloadProjectWorkspaceFile(ctx, input.ProjectID, input.RelativePath)
	if err != nil {
		return nil, projectWorkspaceHTTPErrorInternal(err)
	}
	return &huma.StreamResponse{Body: func(humaCtx huma.Context) {
		defer func() { _ = reader.Close() }()
		humaCtx.SetHeader("Content-Type", "application/octet-stream")
		humaCtx.SetHeader("Content-Disposition", "attachment; filename="+name)
		humaCtx.SetHeader("Content-Length", strconv.FormatInt(size, 10))
		_, _ = io.Copy(humaCtx.BodyWriter(), reader)
	}}, nil
}

func (h *ProjectHandler) UpdateProjectWorkspace(ctx context.Context, input *UpdateProjectWorkspaceInput) (*handlerutil.Out[workspacetypes.Workspace], error) {
	manifest, err := handlerutil.ParseMultipartJSONPart[projecttypes.WorkspaceUpdateManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	maxFileSizeMB := workspacepkg.DefaultMaxFileSizeMB
	if h.projectService != nil && h.projectService.config != nil {
		maxFileSizeMB = h.projectService.config.ProjectWorkspaceMaxFileSizeMB
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, workspacepkg.MaxFileSizeBytes(maxFileSizeMB))
	if err != nil {
		return nil, err
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	var result *workspacetypes.Workspace
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID: input.EnvironmentID, Type: activitytypes.TypeResourceAction, ResourceType: "project", ResourceID: input.ProjectID, ResourceName: input.ProjectID, User: user,
		Step: "Updating project workspace", Message: "Updating project workspace", SuccessMessage: "Project workspace updated successfully",
		Metadata: database.JSON{"action": "update_project_workspace", "fileChangeCount": len(manifest.FileChanges)},
	}, func(runtimeCtx context.Context) error {
		var updateErr error
		result, updateErr = h.projectService.UpdateProjectWorkspace(runtimeCtx, input.ProjectID, manifest, uploads, *user)
		return updateErr
	})
	if err != nil {
		return nil, projectWorkspaceHTTPErrorInternal(err)
	}
	result.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[workspacetypes.Workspace]{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: *result}}, nil
}
