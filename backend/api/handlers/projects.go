package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/volumes"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/projects"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/project"
)

// ProjectHandler provides Huma-based project management endpoints.
type ProjectHandler struct {
	projectService  *services.ProjectService
	activityService *services.ActivityService
	appCtx          context.Context
}

// --- Huma Input/Output Wrappers ---

// ProjectPaginatedResponse is the paginated response for projects.
type ProjectPaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []project.Details       `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListProjectsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
	Status        string `query:"status" doc:"Filter by status (comma-separated: running,stopped,partially running)"`
	Updates       string `query:"updates" doc:"Filter by update status (has_update, up_to_date, error, unknown)"`
	Archived      string `query:"archived" doc:"Archived filter: 'true' (only archived), 'all' (include archived). Default excludes archived."`
}

type ListProjectsOutput struct {
	Body ProjectPaginatedResponse
}

type GetProjectStatusCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetProjectStatusCountsOutput struct {
	Body base.ApiResponse[project.StatusCounts]
}

type DeployProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.DeployOptions
}

type DeployProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DownProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type DownProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type CreateProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          project.CreateProject
}

type CreateProjectOutput struct {
	Body base.ApiResponse[project.CreateReponse]
}

type GetProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type GetProjectOutput struct {
	Body base.ApiResponse[project.Details]
}

type GetProjectFileInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	RelativePath  string `query:"relativePath" doc:"Path to the file relative to the project"`
}

type GetProjectFileOutput struct {
	Body base.ApiResponse[project.IncludeFile]
}

type RedeployProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.DeployOptions
}

type RedeployProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DestroyProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.Destroy
}

type DestroyProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type UpdateProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          project.UpdateProject
}

type UpdateProjectOutput struct {
	Body base.ApiResponse[project.Details]
}

type UpdateProjectIncludeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          project.UpdateIncludeFile
}

type UpdateProjectIncludeOutput struct {
	Body base.ApiResponse[project.Details]
}

type RestartProjectInput struct {
	EnvironmentID string   `path:"id" doc:"Environment ID"`
	ProjectID     string   `path:"projectId" doc:"Project ID"`
	Services      []string `query:"services" doc:"Service names to restart; empty restarts all services"`
}

type RestartProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type UpdateProjectServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *struct {
		Services []string `json:"services,omitempty" doc:"Service names to update; empty updates all services"`
	}
}

type UpdateProjectServicesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type ArchiveProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type ArchiveProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type UnarchiveProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type UnarchiveProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PullProjectImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type BuildProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *struct {
		Services []string `json:"services,omitempty" doc:"Service names to build"`
		Provider string   `json:"provider,omitempty" doc:"Build provider override"`
		Push     *bool    `json:"push,omitempty" doc:"Push images"`
		Load     *bool    `json:"load,omitempty" doc:"Load images into Docker"`
	}
}

// PullProgressEvent represents a Docker pull progress event
type PullProgressEvent struct {
	Status         string `json:"status,omitempty"`
	ID             string `json:"id,omitempty"`
	Progress       string `json:"progress,omitempty"`
	ProgressDetail struct {
		Current int64 `json:"current,omitempty"`
		Total   int64 `json:"total,omitempty"`
	} `json:"progressDetail"`
	Error string `json:"error,omitempty"`
}

// RegisterProjects registers project management routes using Huma.
// Note: WebSocket and streaming endpoints remain as Gin handlers.
//
//nolint:maintidx // long but flat Huma route-registration function; complexity is sequential, not branching
func RegisterProjects(api huma.API, projectService *services.ProjectService, activityService *services.ActivityService, appCtx ActivityAppContext) {
	h := &ProjectHandler{
		projectService:  projectService,
		activityService: activityService,
		appCtx:          appCtx.contextInternal(),
	}

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects",
		Summary:     "List projects",
		Description: "Get a paginated list of Docker Compose projects",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsList, h.ListProjects)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-status-counts",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/counts",
		Summary:     "Get project status counts",
		Description: "Get counts of running, stopped, and total projects",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsList, h.GetProjectStatusCounts)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "deploy-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/up",
		Summary:     "Deploy a project",
		Description: "Deploy a Docker Compose project (docker-compose up)",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsDeploy, h.DeployProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "down-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/down",
		Summary:     "Bring down a project",
		Description: "Bring down a Docker Compose project (docker-compose down)",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsDown, h.DownProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "create-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects",
		Summary:     "Create a project",
		Description: "Create a new Docker Compose project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsCreate, h.CreateProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Get a project",
		Description: "Get a Docker Compose project by ID",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRead, h.GetProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-compose",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/compose",
		Summary:     "Get project compose details",
		Description: "Get compose content, includes, and service configs for a project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRead, h.GetProjectCompose)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-files",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/files",
		Summary:     "Get project files",
		Description: "Get directory files for a project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRead, h.GetProjectFiles)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-runtime",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/runtime",
		Summary:     "Get project runtime",
		Description: "Get runtime service state for a project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRead, h.GetProjectRuntime)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-updates",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/updates",
		Summary:     "Get project updates",
		Description: "Get image update summary for a project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRead, h.GetProjectUpdates)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-project-file",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/projects/{projectId}/file",
		Summary:     "Get a project file",
		Description: "Get the contents of a single project-related file by relative path",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRead, h.GetProjectFile)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "redeploy-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/redeploy",
		Summary:     "Redeploy a project",
		Description: "Redeploy a Docker Compose project (down + up)",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsDeploy, h.RedeployProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "destroy-project",
		Method:      http.MethodDelete,
		Path:        "/environments/{id}/projects/{projectId}/destroy",
		Summary:     "Destroy a project",
		Description: "Destroy a Docker Compose project and optionally remove files/volumes",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsDelete, h.DestroyProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/projects/{projectId}",
		Summary:     "Update a project",
		Description: "Update a Docker Compose project configuration",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsUpdate, h.UpdateProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-include",
		Method:      http.MethodPut,
		Path:        "/environments/{id}/projects/{projectId}/includes",
		Summary:     "Update project include file",
		Description: "Update an include file within a Docker Compose project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsUpdate, h.UpdateProjectInclude)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "restart-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/restart",
		Summary:     "Restart a project",
		Description: "Restart all containers in a Docker Compose project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsRestart, h.RestartProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "update-project-services",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/update-services",
		Summary:     "Update project services",
		Description: "Pull latest images and recreate the given services (all services when none are specified)",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsUpdate, h.UpdateProjectServices)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "archive-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/archive",
		Summary:     "Archive a project",
		Description: "Archive a stopped Docker Compose project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsArchive, h.ArchiveProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "unarchive-project",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/unarchive",
		Summary:     "Unarchive a project",
		Description: "Unarchive a Docker Compose project",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsArchive, h.UnarchiveProject)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "pull-project-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/pull",
		Summary:     "Pull project images",
		Description: "Pull all images for a Docker Compose project with streaming progress output",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsDeploy, h.PullProjectImages)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "build-project-images",
		Method:      http.MethodPost,
		Path:        "/environments/{id}/projects/{projectId}/build",
		Summary:     "Build project images",
		Description: "Build Docker Compose services with build directives using BuildKit",
		Tags:        []string{"Projects"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermProjectsDeploy, h.BuildProjectImages)
}

// ListProjects returns a paginated list of projects.
func (h *ProjectHandler) ListProjects(ctx context.Context, input *ListProjectsInput) (*ListProjectsOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}
	if input.Archived != "" {
		params.Filters["archived"] = input.Archived
	}

	projects, paginationResp, err := h.projectService.ListProjects(ctx, params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, huma.Error500InternalServerError("Request was canceled")
		}
		return nil, huma.Error500InternalServerError((&common.ProjectListError{Err: err}).Error())
	}

	if projects == nil {
		projects = []project.Details{}
	}

	return &ListProjectsOutput{
		Body: ProjectPaginatedResponse{
			Success:    true,
			Data:       projects,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetProjectStatusCounts returns counts of projects by status.
func (h *ProjectHandler) GetProjectStatusCounts(ctx context.Context, input *GetProjectStatusCountsInput) (*GetProjectStatusCountsOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	_, running, stopped, total, archived, err := h.projectService.GetProjectStatusCounts(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectStatusCountsError{Err: err}).Error())
	}

	return &GetProjectStatusCountsOutput{
		Body: base.ApiResponse[project.StatusCounts]{
			Success: true,
			Data: project.StatusCounts{
				RunningProjects:  running,
				StoppedProjects:  stopped,
				TotalProjects:    total,
				ArchivedProjects: archived,
			},
		},
	}, nil
}

// DeployProject deploys a Docker Compose project.
func (h *ProjectHandler) DeployProject(ctx context.Context, input *DeployProjectInput) (*huma.StreamResponse, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				models.ActivityTypeProjectDeploy,
				"project",
				input.ProjectID,
				input.ProjectID,
				user,
				"Starting deployment",
				"Project deployment started",
				models.JSON{"projectID": input.ProjectID},
			)
			activitylib.WriteStartedLine(rawWriter, activityID)
			if f, ok := rawWriter.(http.Flusher); ok {
				f.Flush()
			}

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Deploying project")
			_, _ = writer.Write([]byte(`{"type":"deploy","phase":"begin"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}

			deployCtx := context.WithValue(runtimeCtx, projects.ProgressWriterKey{}, writer)
			if err := h.projectService.DeployProject(deployCtx, input.ProjectID, *user, input.Body); err != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project deployment failed", err)
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
				return
			}

			_, _ = writer.Write([]byte(`{"type":"deploy","phase":"complete"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project deployment completed", nil)
		},
	}, nil
}

// DownProject brings down a Docker Compose project.
func (h *ProjectHandler) DownProject(ctx context.Context, input *DownProjectInput) (*DownProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(runtimeCtx, h.activityService, input.EnvironmentID, models.ActivityTypeProjectDown, "project", input.ProjectID, input.ProjectID, user, "Stopping project", "Project stop requested", models.JSON{"projectID": input.ProjectID})
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Stopping project")
	downCtx := context.WithValue(runtimeCtx, projects.ProgressWriterKey{}, activityWriter)
	if err := h.projectService.DownProject(downCtx, input.ProjectID, *user); err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project stopped", err)
		if _, ok := errors.AsType[*common.ProjectArchivedError](err); ok {
			return nil, huma.Error400BadRequest((&common.ProjectDownError{Err: err}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.ProjectDownError{Err: err}).Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project stopped", nil)

	return &DownProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message:    "Project brought down successfully",
				ActivityID: utils.StringPtrFromTrimmed(activityID),
			},
		},
	}, nil
}

func projectUpdateHTTPErrorInternal(err error) error {
	if conflictErr, ok := errors.AsType[*volumes.ProjectVolumeRenameConflictError](err); ok {
		return huma.Error409Conflict(conflictErr.Error())
	}
	if inUseErr, ok := errors.AsType[*volumes.ProjectVolumeRenameInUseError](err); ok {
		return huma.Error409Conflict(inUseErr.Error())
	}
	if spaceErr, ok := errors.AsType[*volumes.ProjectVolumeRenameInsufficientSpaceError](err); ok {
		return huma.NewError(http.StatusInsufficientStorage, spaceErr.Error())
	}
	return projectFileHTTPError(err)
}

// projectFileHTTPError maps project file management errors to HTTP errors.
// It returns nil when err is not a project file error.
func projectFileHTTPError(err error) error {
	if conflictErr, ok := errors.AsType[*common.ProjectFileConflictError](err); ok {
		return huma.Error409Conflict(conflictErr.Error())
	}
	if forbiddenErr, ok := errors.AsType[*common.ProjectFileForbiddenError](err); ok {
		return huma.Error403Forbidden(forbiddenErr.Error())
	}
	if badRequestErr, ok := errors.AsType[*common.ProjectFileBadRequestError](err); ok {
		return huma.Error400BadRequest(badRequestErr.Error())
	}
	return nil
}

// CreateProject creates a new Docker Compose project.
func (h *ProjectHandler) CreateProject(ctx context.Context, input *CreateProjectInput) (*CreateProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	var proj *models.Project
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "project",
		ResourceID:     input.Body.Name,
		ResourceName:   input.Body.Name,
		User:           user,
		Step:           "Creating project",
		Message:        "Creating project",
		SuccessMessage: "Project created successfully",
		Metadata:       models.JSON{"action": "create_project"},
	}, func(runtimeCtx context.Context) error {
		var createErr error
		proj, createErr = h.projectService.CreateProject(runtimeCtx, input.Body.Name, input.Body.ComposeContent, input.Body.EnvContent, input.Body.ProjectFiles, *user)
		return createErr
	})
	if err != nil {
		if httpErr := projectFileHTTPError(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError((&common.ProjectCreationError{Err: err}).Error())
	}

	var response project.CreateReponse
	if err := mapper.MapStruct(proj, &response); err != nil {
		return nil, huma.Error500InternalServerError("failed to map response")
	}
	response.Status = string(proj.Status)
	response.StatusReason = proj.StatusReason
	response.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	response.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	response.DirName = utils.DerefString(proj.DirName)
	response.RelativePath = h.projectService.GetProjectRelativePath(ctx, proj.Path)
	response.GitOpsManagedBy = proj.GitOpsManagedBy
	response.IsArchived = proj.IsArchived
	response.ArchivedAt = proj.ArchivedAt
	response.ActivityID = utils.StringPtrFromTrimmed(activityID)

	return &CreateProjectOutput{
		Body: base.ApiResponse[project.CreateReponse]{
			Success: true,
			Data:    response,
		},
	}, nil
}

// GetProject returns a project by ID.
func (h *ProjectHandler) GetProject(ctx context.Context, input *GetProjectInput) (*GetProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID, project.DetailsOptions{})
	if err != nil {
		return nil, huma.Error404NotFound((&common.ProjectDetailsError{Err: err}).Error())
	}

	return &GetProjectOutput{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ProjectHandler) getProjectDetailsWithOptionsInternal(ctx context.Context, input *GetProjectInput, opts project.DetailsOptions) (*GetProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID, opts)
	if err != nil {
		return nil, huma.Error404NotFound((&common.ProjectDetailsError{Err: err}).Error())
	}

	return &GetProjectOutput{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ProjectHandler) GetProjectCompose(ctx context.Context, input *GetProjectInput) (*GetProjectOutput, error) {
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
		IncludeComposeContent: true,
		IncludeEnvState:       true,
		IncludeIncludeFiles:   true,
		IncludeServiceConfigs: true,
	})
}

func (h *ProjectHandler) GetProjectFiles(ctx context.Context, input *GetProjectInput) (*GetProjectOutput, error) {
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
		IncludeDirectoryFiles: true,
		IncludeProjectFiles:   true,
	})
}

func (h *ProjectHandler) GetProjectRuntime(ctx context.Context, input *GetProjectInput) (*GetProjectOutput, error) {
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
		IncludeRuntimeServices: true,
	})
}

func (h *ProjectHandler) GetProjectUpdates(ctx context.Context, input *GetProjectInput) (*GetProjectOutput, error) {
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
		IncludeServiceConfigs: true,
		IncludeUpdateInfo:     true,
	})
}

func (h *ProjectHandler) GetProjectFile(ctx context.Context, input *GetProjectFileInput) (*GetProjectFileOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}
	if input.RelativePath == "" {
		return nil, huma.Error400BadRequest("relativePath is required")
	}

	file, err := h.projectService.GetProjectFileContent(ctx, input.ProjectID, input.RelativePath)
	if err != nil {
		var badRequestErr *common.ProjectFileBadRequestError
		var forbiddenErr *common.ProjectFileForbiddenError
		var notFoundErr *common.ProjectFileNotFoundError

		switch {
		case errors.As(err, &badRequestErr):
			return nil, huma.Error400BadRequest(err.Error())
		case errors.As(err, &forbiddenErr):
			return nil, huma.Error403Forbidden(err.Error())
		case errors.As(err, &notFoundErr):
			return nil, huma.Error404NotFound("project file not found")
		default:
			return nil, huma.Error500InternalServerError("internal error")
		}
	}

	return &GetProjectFileOutput{
		Body: base.ApiResponse[project.IncludeFile]{
			Success: true,
			Data:    file,
		},
	}, nil
}

// RedeployProject redeploys a Docker Compose project.
func (h *ProjectHandler) RedeployProject(ctx context.Context, input *RedeployProjectInput) (*RedeployProjectOutput, error) {
	response, err := h.runProjectActivityActionResponseInternal(ctx, input.EnvironmentID, input.ProjectID, h.redeployProjectActivityConfigInternal(input.Body))
	if err != nil {
		return nil, err
	}

	return &RedeployProjectOutput{
		Body: response,
	}, nil
}

// DestroyProject destroys a Docker Compose project.
func (h *ProjectHandler) DestroyProject(ctx context.Context, input *DestroyProjectInput) (*DestroyProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	removeFiles := true
	removeVolumes := false
	if input.Body != nil {
		if input.Body.RemoveFiles != nil {
			removeFiles = *input.Body.RemoveFiles
		}
		removeVolumes = input.Body.RemoveVolumes
		slog.DebugContext(ctx, "DestroyProject handler received body",
			"removeFiles", removeFiles,
			"removeVolumes", removeVolumes,
			"projectID", input.ProjectID)
	} else {
		slog.DebugContext(ctx, "DestroyProject handler received nil body",
			"projectID", input.ProjectID)
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(runtimeCtx, h.activityService, input.EnvironmentID, models.ActivityTypeProjectDestroy, "project", input.ProjectID, input.ProjectID, user, "Destroying project", "Project destroy requested", models.JSON{"projectID": input.ProjectID, "removeFiles": removeFiles, "removeVolumes": removeVolumes})
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Destroying project")
	destroyCtx := context.WithValue(runtimeCtx, projects.ProgressWriterKey{}, activityWriter)
	if err := h.projectService.DestroyProject(destroyCtx, input.ProjectID, removeFiles, removeVolumes, *user); err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project destroyed", err)
		return nil, huma.Error500InternalServerError((&common.ProjectDestroyError{Err: err}).Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project destroyed", nil)

	return &DestroyProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message:    "Project destroyed successfully",
				ActivityID: utils.StringPtrFromTrimmed(activityID),
			},
		},
	}, nil
}

// UpdateProject updates a Docker Compose project.
func (h *ProjectHandler) UpdateProject(ctx context.Context, input *UpdateProjectInput) (*UpdateProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "project",
		ResourceID:     input.ProjectID,
		ResourceName:   utils.DerefString(input.Body.Name),
		User:           user,
		Step:           "Updating project",
		Message:        "Updating project",
		SuccessMessage: "Project updated successfully",
		Metadata:       models.JSON{"action": "update_project", "projectID": input.ProjectID},
	}, func(runtimeCtx context.Context) error {
		_, updateErr := h.projectService.UpdateProject(runtimeCtx, input.ProjectID, input.Body.Name, input.Body.ComposeContent, input.Body.EnvContent, input.Body.FileTreeRevision, input.Body.FileChanges, *user)
		return updateErr
	})
	if err != nil {
		if httpErr := projectUpdateHTTPErrorInternal(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error400BadRequest((&common.ProjectUpdateError{Err: err}).Error())
	}

	// Skip the recursive directory walks on save: the file tree is only
	// re-read when the save actually staged file changes (fresh revision),
	// and the frontend fetches /files lazily otherwise.
	details, err := h.projectService.GetProjectDetails(runtimeCtx, input.ProjectID, project.DetailsOptions{
		IncludeComposeContent:  true,
		IncludeEnvState:        true,
		IncludeIncludeFiles:    true,
		IncludeServiceConfigs:  true,
		IncludeProjectFiles:    len(input.Body.FileChanges) > 0,
		IncludeRuntimeServices: true,
		IncludeUpdateInfo:      true,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectDetailsError{Err: err}).Error())
	}
	details.ActivityID = utils.StringPtrFromTrimmed(activityID)

	return &UpdateProjectOutput{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

// UpdateProjectInclude updates an include file within a project.
func (h *ProjectHandler) UpdateProjectInclude(ctx context.Context, input *UpdateProjectIncludeInput) (*UpdateProjectIncludeOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           models.ActivityTypeResourceAction,
		ResourceType:   "project",
		ResourceID:     input.ProjectID,
		ResourceName:   input.ProjectID,
		User:           user,
		Step:           "Updating project file",
		Message:        "Updating project include file",
		SuccessMessage: "Project file updated successfully",
		Metadata: models.JSON{
			"action":       "update_project_include",
			"projectID":    input.ProjectID,
			"relativePath": input.Body.RelativePath,
		},
	}, func(runtimeCtx context.Context) error {
		return h.projectService.UpdateProjectIncludeFile(runtimeCtx, input.ProjectID, input.Body.RelativePath, input.Body.Content, *user)
	})
	if err != nil {
		return nil, huma.Error400BadRequest((&common.ProjectUpdateError{Err: err}).Error())
	}

	details, err := h.projectService.GetProjectDetails(runtimeCtx, input.ProjectID, project.DetailsOptions{
		IncludeComposeContent:  true,
		IncludeEnvState:        true,
		IncludeIncludeFiles:    true,
		IncludeServiceConfigs:  true,
		IncludeRuntimeServices: true,
		IncludeUpdateInfo:      true,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectDetailsError{Err: err}).Error())
	}
	details.ActivityID = utils.StringPtrFromTrimmed(activityID)

	return &UpdateProjectIncludeOutput{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

// RestartProject restarts the given services in a project (all services when none
// are specified).
func (h *ProjectHandler) RestartProject(ctx context.Context, input *RestartProjectInput) (*RestartProjectOutput, error) {
	response, err := h.runProjectActivityActionResponseInternal(ctx, input.EnvironmentID, input.ProjectID, h.restartProjectActivityConfigInternal(input.Services))
	if err != nil {
		return nil, err
	}

	return &RestartProjectOutput{
		Body: response,
	}, nil
}

// UpdateProjectServices pulls the latest images for the given services and recreates them.
func (h *ProjectHandler) UpdateProjectServices(ctx context.Context, input *UpdateProjectServicesInput) (*UpdateProjectServicesOutput, error) {
	var services []string
	if input.Body != nil {
		services = input.Body.Services
	}

	response, err := h.runProjectActivityActionResponseInternal(ctx, input.EnvironmentID, input.ProjectID, h.updateProjectServicesActivityConfigInternal(services))
	if err != nil {
		return nil, err
	}

	return &UpdateProjectServicesOutput{
		Body: response,
	}, nil
}

type projectActivityActionConfigInternal struct {
	ActivityType    models.ActivityType
	Step            string
	StartMessage    string
	WriterStep      string
	FailureMessage  string
	SuccessComplete string
	SuccessMessage  string
	Action          func(context.Context, string, models.User) error
	Error           func(error) error
}

func (h *ProjectHandler) redeployProjectActivityConfigInternal(options *project.DeployOptions) projectActivityActionConfigInternal {
	return projectActivityActionConfigInternal{
		ActivityType:    models.ActivityTypeProjectRedeploy,
		Step:            "Starting redeploy",
		StartMessage:    "Project redeploy started",
		WriterStep:      "Redeploying project",
		FailureMessage:  "Project redeploy failed",
		SuccessComplete: "Project redeploy completed",
		SuccessMessage:  "Project redeployed successfully",
		Action: func(runtimeCtx context.Context, projectID string, user models.User) error {
			return h.projectService.RedeployProject(runtimeCtx, projectID, user, options)
		},
		Error: projectArchivedActionErrorInternal(func(err error) error {
			return huma.Error400BadRequest((&common.ProjectRedeploymentError{Err: err}).Error())
		}),
	}
}

func (h *ProjectHandler) updateProjectServicesActivityConfigInternal(services []string) projectActivityActionConfigInternal {
	return projectActivityActionConfigInternal{
		ActivityType:    models.ActivityTypeAutoUpdate,
		Step:            "Updating project services",
		StartMessage:    "Project services update requested",
		WriterStep:      "Updating project services",
		FailureMessage:  "Project services update failed",
		SuccessComplete: "Project services updated",
		SuccessMessage:  "Project services updated successfully",
		Action: func(runtimeCtx context.Context, projectID string, user models.User) error {
			return h.projectService.UpdateProjectServices(runtimeCtx, projectID, services, user)
		},
		Error: projectArchivedActionErrorInternal(func(err error) error {
			return huma.Error400BadRequest((&common.ProjectUpdateError{Err: err}).Error())
		}),
	}
}

func (h *ProjectHandler) restartProjectActivityConfigInternal(services []string) projectActivityActionConfigInternal {
	return projectActivityActionConfigInternal{
		ActivityType:    models.ActivityTypeProjectRestart,
		Step:            "Restarting project",
		StartMessage:    "Project restart requested",
		WriterStep:      "Restarting project",
		FailureMessage:  "Project restarted",
		SuccessComplete: "Project restarted",
		SuccessMessage:  "Project restarted successfully",
		Action: func(runtimeCtx context.Context, projectID string, user models.User) error {
			return h.projectService.RestartProject(runtimeCtx, projectID, services, user)
		},
		Error: projectArchivedActionErrorInternal(func(err error) error {
			return huma.Error400BadRequest((&common.ProjectRestartError{Err: err}).Error())
		}),
	}
}

func projectArchivedActionErrorInternal(fallback func(error) error) func(error) error {
	return func(err error) error {
		if _, ok := errors.AsType[*common.ProjectArchivedError](err); ok {
			return huma.Error400BadRequest(err.Error())
		}
		return fallback(err)
	}
}

func (h *ProjectHandler) runProjectActivityActionResponseInternal(
	ctx context.Context,
	environmentID string,
	projectID string,
	cfg projectActivityActionConfigInternal,
) (base.ApiResponse[base.MessageResponse], error) {
	message, err := h.runProjectActivityActionInternal(ctx, environmentID, projectID, cfg)
	if err != nil {
		return base.ApiResponse[base.MessageResponse]{}, err
	}

	return base.ApiResponse[base.MessageResponse]{
		Success: true,
		Data:    message,
	}, nil
}

func (h *ProjectHandler) runProjectActivityActionInternal(ctx context.Context, environmentID, projectID string, cfg projectActivityActionConfigInternal) (base.MessageResponse, error) {
	if h.projectService == nil {
		return base.MessageResponse{}, huma.Error500InternalServerError("service not available")
	}

	if projectID == "" {
		return base.MessageResponse{}, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return base.MessageResponse{}, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(runtimeCtx, h.activityService, environmentID, cfg.ActivityType, "project", projectID, projectID, user, cfg.Step, cfg.StartMessage, models.JSON{"projectID": projectID})
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, cfg.WriterStep)
	actionCtx := context.WithValue(runtimeCtx, projects.ProgressWriterKey{}, activityWriter)
	if err := cfg.Action(actionCtx, projectID, *user); err != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.FailureMessage, err)
		return base.MessageResponse{}, cfg.Error(err)
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.SuccessComplete, nil)

	return base.MessageResponse{
		Message:    cfg.SuccessMessage,
		ActivityID: utils.StringPtrFromTrimmed(activityID),
	}, nil
}

func (h *ProjectHandler) ArchiveProject(ctx context.Context, input *ArchiveProjectInput) (*ArchiveProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.projectService.ArchiveProject(ctx, input.ProjectID, *user); err != nil {
		if _, ok := errors.AsType[*common.ProjectMustBeStoppedError](err); ok {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.ProjectArchiveError{Err: err}).Error())
	}

	return &ArchiveProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Project archived successfully"},
		},
	}, nil
}

func (h *ProjectHandler) UnarchiveProject(ctx context.Context, input *UnarchiveProjectInput) (*UnarchiveProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	if err := h.projectService.UnarchiveProject(ctx, input.ProjectID, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectUnarchiveError{Err: err}).Error())
	}

	return &UnarchiveProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Project unarchived successfully"},
		},
	}, nil
}

// PullProjectImages pulls all images for a project with streaming progress.
func (h *ProjectHandler) PullProjectImages(ctx context.Context, input *PullProjectImagesInput) (*huma.StreamResponse, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				models.ActivityTypeProjectPull,
				"project",
				input.ProjectID,
				input.ProjectID,
				user,
				"Pulling project images",
				"Project image pull started",
				models.JSON{"projectID": input.ProjectID},
			)
			activitylib.WriteStartedLine(rawWriter, activityID)

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Pulling project images")
			_, _ = writer.Write([]byte(`{"status":"starting project image pull"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}

			if err := h.projectService.PullProjectImages(runtimeCtx, input.ProjectID, writer, *user, nil); err != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project image pull failed", err)
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
				return
			}

			_, _ = writer.Write([]byte(`{"status":"complete"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project image pull completed", nil)
		},
	}, nil
}

// BuildProjectImages builds compose services with build directives.
func (h *ProjectHandler) BuildProjectImages(ctx context.Context, input *BuildProjectInput) (*huma.StreamResponse, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, err := requireUserInternal(ctx)
	if err != nil {
		return nil, err
	}

	options := services.ProjectBuildOptions{}
	if input.Body != nil {
		options.Services = input.Body.Services
		options.Provider = input.Body.Provider
		options.Push = input.Body.Push
		options.Load = input.Body.Load
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			activityID, runtimeCtx := activitylib.StartHandlerActivityForUser(
				runtimeCtx,
				h.activityService,
				input.EnvironmentID,
				models.ActivityTypeProjectBuild,
				"project",
				input.ProjectID,
				input.ProjectID,
				user,
				"Building project images",
				"Project image build started",
				models.JSON{"projectID": input.ProjectID, "services": options.Services},
			)
			activitylib.WriteStartedLine(rawWriter, activityID)

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, "Building project images")
			_, _ = writer.Write([]byte(`{"type":"build","phase":"begin"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}

			if err := h.projectService.BuildProjectServices(runtimeCtx, input.ProjectID, options, writer, user); err != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project image build failed", err)
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
				return
			}

			_, _ = writer.Write([]byte(`{"type":"build","phase":"complete"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project image build completed", nil)
		},
	}, nil
}
