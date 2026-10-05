// Package project owns compose project persistence, lifecycle, discovery, and routes.
package project

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
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
	registerProjectWorkspaceRoutesInternal(api, h)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects",
		Summary:     "List projects",
		Description: "Get a paginated list of Docker Compose projects",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsList, h.ListProjects)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/counts",
		Summary:     "Get project status counts",
		Description: "Get counts of running, stopped, and total projects",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsList, h.GetProjectStatusCounts)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-project-tags",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/tags",
		Summary:     "List project tags",
		Description: "Get sorted, distinct project tag names",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
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

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deploy-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/up",
		Summary:     "Deploy a project",
		Description: "Deploy a Docker Compose project (docker-compose up)",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDeploy, h.DeployProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "down-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/down",
		Summary:     "Bring down a project",
		Description: "Bring down a Docker Compose project (docker-compose down)",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDown, h.DownProject)

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
		OperationID: "redeploy-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/redeploy",
		Summary:     "Redeploy a project",
		Description: "Redeploy a Docker Compose project (down + up)",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDeploy, h.RedeployProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "destroy-project",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/projects/{projectId}/destroy",
		Summary:     "Destroy a project",
		Description: "Destroy a Docker Compose project and optionally remove files/volumes",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDelete, h.DestroyProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Update a project",
		Description: "Update a Docker Compose project configuration",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.UpdateProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "restart-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/restart",
		Summary:     "Restart a project",
		Description: "Restart all containers in a Docker Compose project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsRestart, h.RestartProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-services",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/update-services",
		Summary:     "Update project services",
		Description: "Pull latest images and recreate the given services (all services when none are specified)",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsUpdate, h.UpdateProjectServices)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "archive-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/archive",
		Summary:     "Archive a project",
		Description: "Archive a stopped Docker Compose project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsArchive, h.ArchiveProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "unarchive-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/unarchive",
		Summary:     "Unarchive a project",
		Description: "Unarchive a Docker Compose project",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsArchive, h.UnarchiveProject)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pull-project-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/pull",
		Summary:     "Pull project images",
		Description: "Pull all images for a Docker Compose project with streaming progress output",
		Tags:        []string{"Projects"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermProjectsDeploy, h.PullProjectImages)

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

func registerProjectWorkspaceRoutesInternal(api huma.API, h *ProjectHandler) {
	basePath := "/environments/{id}/projects/{projectId}/workspace"
	tag := "Project Workspace"
	handlerutil.RegisterSecured(api, handlerutil.Operation("get-project-workspace", http.MethodGet, basePath, "Get project workspace", "", tag), authz.PermProjectsRead, h.GetProjectWorkspace)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"get-project-workspace-file",
			http.MethodGet,
			basePath+"/file",
			"Get project workspace file",
			"",
			tag,
		),
		authz.PermProjectsRead,
		h.GetProjectWorkspaceFile,
	)
	handlerutil.RegisterSecured(
		api,
		handlerutil.Operation(
			"download-project-workspace-file",
			http.MethodGet,
			basePath+"/file/download",
			"Download project workspace file",
			"",
			tag,
		),
		authz.PermProjectsRead,
		h.DownloadProjectWorkspaceFile,
	)
	updateOperation := handlerutil.Operation("update-project-workspace", http.MethodPut, basePath, "Update project workspace", "", tag)
	updateOperation.RequestBody = handlerutil.WorkspaceMultipartRequestBody("JSON encoded project workspace manifest")
	handlerutil.RegisterSecured(api, updateOperation, authz.PermProjectsUpdate, h.UpdateProjectWorkspace)
}
