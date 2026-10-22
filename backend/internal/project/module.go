// Package project owns compose project persistence, lifecycle, discovery, and routes.
package project

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service  *ProjectService
	activity *activity.ActivityService
}

func New(service *ProjectService, activityService *activity.ActivityService) *Module {
	return &Module{service: service, activity: activityService}
}

func (m *Module) Service() *ProjectService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, appCtx handlerutil.ActivityAppContext) {
	if m == nil {
		RegisterProjects(api, nil, nil, appCtx)
		return
	}
	RegisterProjects(api, m.service, m.activity, appCtx)
}

// RegisterProjects registers project management routes using Huma.
// WebSocket and streaming endpoints live in api/ws.
func RegisterProjects(api huma.API, projectService *ProjectService, activityService *activity.ActivityService, appCtx handlerutil.ActivityAppContext) {
	h := &ProjectHandler{
		projectService:  projectService,
		activityService: activityService,
		appCtx:          appCtx.Context(),
	}
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-workspace",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/workspace",
		Summary:     "Get project workspace",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectWorkspace)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-workspace-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/workspace/file",
		Summary:     "Get project workspace file",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectWorkspaceFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "download-project-workspace-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/workspace/file/download",
		Summary:     "Download project workspace file",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.DownloadProjectWorkspaceFile)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-workspace",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/projects/{projectId}/workspace",
		Summary:     "Update project workspace",
		Tags:        []string{"Project Workspace"},
		Security:    handlerutil.DefaultOperationSecurity(),
		RequestBody: handlerutil.WorkspaceMultipartRequestBody("JSON encoded project workspace manifest"),
	}, authz.PermProjectsUpdate, h.UpdateProjectWorkspace)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects",
		Summary:     "List projects",
		Description: "Get a paginated list of Docker Compose projects",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermProjectsList, h.ListProjects)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/counts",
		Summary:     "Get project status counts",
		Description: "Get counts of running, stopped, and total projects",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermProjectsList, h.GetProjectStatusCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-project-tags",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/tags",
		Summary:     "List project tags",
		Description: "Get sorted, distinct project tag names",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermProjectsList, h.ListProjectTags)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-project-references",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/references",
		Summary:     "List project references",
		Description: "Get the ID and name of every project, including archived projects, without runtime state",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsList, h.ListProjectReferences)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-tag",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/projects/{projectId}/tags",
		Summary:     "Update a project tag",
		Description: "Attach or detach a UI-managed project tag",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.UpdateProjectTag)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "deploy-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/deploy",
		Summary:     "Deploy, redeploy, or pull a project",
		Description: "Stream a Docker Compose deploy action: up (docker compose up), redeploy (pull + down + up), or pull (pull all project images). " +
			"The optional deploy options body applies to up and redeploy; pull ignores it.",
		Tags:     []string{"Projects"},
		Security: handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"up":       authz.PermProjectsDeploy,
		"redeploy": authz.PermProjectsDeploy,
		"pull":     authz.PermProjectsDeploy,
	}, h.DeployProject)

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "project-action",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Run project action",
		Description: "Bring down, restart, archive, unarchive, or update services of a Docker Compose project. Restart and updateservices accept an optional services list (empty targets all services).",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"down":           authz.PermProjectsDown,
		"restart":        authz.PermProjectsRestart,
		"archive":        authz.PermProjectsArchive,
		"unarchive":      authz.PermProjectsArchive,
		"updateservices": authz.PermProjectsUpdate,
	}, h.RunProjectAction)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects",
		Summary:     "Create a project",
		Description: "Create a new Docker Compose project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
		RequestBody: &huma.RequestBody{
			Content: map[string]*huma.MediaType{
				"multipart/form-data": {
					Schema: &huma.Schema{
						Type: "object",
						Properties: map[string]*huma.Schema{
							"project":  {Type: "string", Description: "JSON encoded project configuration"},
							"manifest": {Type: "string", Description: "JSON encoded initial project workspace manifest"},
							"files":    {Type: "array", Items: &huma.Schema{Type: "string", Format: "binary"}},
						},
						Required: []string{"project", "manifest"},
					},
				},
			},
		},
	}, authz.PermProjectsCreate, h.CreateProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Get a project",
		Description: "Get a Docker Compose project by ID",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-compose",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/compose",
		Summary:     "Get project compose details",
		Description: "Get compose content, includes, and service configs for a project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectCompose)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-runtime",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/runtime",
		Summary:     "Get project runtime",
		Description: "Get runtime service state for a project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectRuntime)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-updates",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/updates",
		Summary:     "Get project updates",
		Description: "Get image update summary for a project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRead, h.GetProjectUpdates)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "destroy-project",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Destroy a project",
		Description: "Destroy a Docker Compose project and optionally remove files/volumes",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDelete, h.DestroyProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project",
		Method:      http.MethodPatch,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Update a project",
		Description: "Update a Docker Compose project configuration",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.UpdateProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "build-project-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/build",
		Summary:     "Build project images",
		Description: "Build Docker Compose services with build directives using BuildKit",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDeploy, h.BuildProjectImages)
}
