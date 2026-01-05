package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/backend/internal/utils/pagination"
	projects "github.com/getarcaneapp/arcane/backend/internal/utils/projects"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/project"
)

// ProjectHandler provides Huma-based project management endpoints.
type ProjectHandler struct {
	projectService *services.ProjectService
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

type RedeployProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
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
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type RestartProjectOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PullProjectImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

// PullProgressEvent represents a Docker pull progress event
type PullProgressEvent struct {
	Status         string `json:"status,omitempty"`
	ID             string `json:"id,omitempty"`
	Progress       string `json:"progress,omitempty"`
	ProgressDetail struct {
		Current int64 `json:"current,omitempty"`
		Total   int64 `json:"total,omitempty"`
	} `json:"progressDetail,omitempty"`
	Error string `json:"error,omitempty"`
}

// RegisterProjects registers project management routes using Huma.
// Note: WebSocket and streaming endpoints remain as Gin handlers.
func RegisterProjects(api huma.API, projectService *services.ProjectService) {
	h := &ProjectHandler{
		projectService: projectService,
	}

	huma.Register(api, huma.Operation{
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
	}, h.ListProjects)

	huma.Register(api, huma.Operation{
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
	}, h.GetProjectStatusCounts)

	huma.Register(api, huma.Operation{
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
	}, h.DeployProject)

	huma.Register(api, huma.Operation{
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
	}, h.DownProject)

	huma.Register(api, huma.Operation{
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
	}, h.CreateProject)

	huma.Register(api, huma.Operation{
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
	}, h.GetProject)

	huma.Register(api, huma.Operation{
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
	}, h.RedeployProject)

	huma.Register(api, huma.Operation{
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
	}, h.DestroyProject)

	huma.Register(api, huma.Operation{
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
	}, h.UpdateProject)

	huma.Register(api, huma.Operation{
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
	}, h.UpdateProjectInclude)

	huma.Register(api, huma.Operation{
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
	}, h.RestartProject)

	huma.Register(api, huma.Operation{
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
	}, h.PullProjectImages)
}

// ListProjects returns a paginated list of projects.
func (h *ProjectHandler) ListProjects(ctx context.Context, input *ListProjectsInput) (*ListProjectsOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: input.Search,
		},
		SortParams: pagination.SortParams{
			Sort:  input.Sort,
			Order: pagination.SortOrder(input.Order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: input.Start,
			Limit: input.Limit,
		},
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
			Success: true,
			Data:    projects,
			Pagination: base.PaginationResponse{
				TotalPages:      paginationResp.TotalPages,
				TotalItems:      paginationResp.TotalItems,
				CurrentPage:     paginationResp.CurrentPage,
				ItemsPerPage:    paginationResp.ItemsPerPage,
				GrandTotalItems: paginationResp.GrandTotalItems,
			},
		},
	}, nil
}

// GetProjectStatusCounts returns counts of projects by status.
func (h *ProjectHandler) GetProjectStatusCounts(ctx context.Context, input *GetProjectStatusCountsInput) (*GetProjectStatusCountsOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	_, running, stopped, total, err := h.projectService.GetProjectStatusCounts(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectStatusCountsError{Err: err}).Error())
	}

	return &GetProjectStatusCountsOutput{
		Body: base.ApiResponse[project.StatusCounts]{
			Success: true,
			Data: project.StatusCounts{
				RunningProjects: int(running),
				StoppedProjects: int(stopped),
				TotalProjects:   int(total),
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

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/x-json-stream")
			humaCtx.SetHeader("Cache-Control", "no-cache")
			humaCtx.SetHeader("Connection", "keep-alive")
			humaCtx.SetHeader("X-Accel-Buffering", "no")

			writer := humaCtx.BodyWriter()

			_, _ = writer.Write([]byte(`{"type":"deploy","phase":"begin"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}

			deployCtx := context.WithValue(humaCtx.Context(), projects.ProgressWriterKey{}, writer)
			if err := h.projectService.DeployProject(deployCtx, input.ProjectID, *user); err != nil {
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
		},
	}, nil
}

// DownProject brings down a Docker Compose project.
func (h *ProjectHandler) DownProject(ctx context.Context, input *DownProjectInput) (*DownProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.projectService.DownProject(ctx, input.ProjectID, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectDownError{Err: err}).Error())
	}

	return &DownProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Project brought down successfully",
			},
		},
	}, nil
}

// CreateProject creates a new Docker Compose project.
func (h *ProjectHandler) CreateProject(ctx context.Context, input *CreateProjectInput) (*CreateProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	proj, err := h.projectService.CreateProject(ctx, input.Body.Name, input.Body.ComposeContent, input.Body.EnvContent, *user)
	if err != nil {
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

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID)
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

// RedeployProject redeploys a Docker Compose project.
func (h *ProjectHandler) RedeployProject(ctx context.Context, input *RedeployProjectInput) (*RedeployProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.projectService.RedeployProject(ctx, input.ProjectID, *user); err != nil {
		return nil, huma.Error400BadRequest((&common.ProjectRedeploymentError{Err: err}).Error())
	}

	return &RedeployProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Project redeployed successfully",
			},
		},
	}, nil
}

// DestroyProject destroys a Docker Compose project.
func (h *ProjectHandler) DestroyProject(ctx context.Context, input *DestroyProjectInput) (*DestroyProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	removeFiles := false
	removeVolumes := false
	if input.Body != nil {
		removeFiles = input.Body.RemoveFiles
		removeVolumes = input.Body.RemoveVolumes
		slog.DebugContext(ctx, "DestroyProject handler received body",
			"removeFiles", removeFiles,
			"removeVolumes", removeVolumes,
			"projectID", input.ProjectID)
	} else {
		slog.DebugContext(ctx, "DestroyProject handler received nil body",
			"projectID", input.ProjectID)
	}

	if err := h.projectService.DestroyProject(ctx, input.ProjectID, removeFiles, removeVolumes, *user); err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectDestroyError{Err: err}).Error())
	}

	return &DestroyProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Project destroyed successfully",
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

	if _, err := h.projectService.UpdateProject(ctx, input.ProjectID, input.Body.Name, input.Body.ComposeContent, input.Body.EnvContent); err != nil {
		return nil, huma.Error400BadRequest((&common.ProjectUpdateError{Err: err}).Error())
	}

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectDetailsError{Err: err}).Error())
	}

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

	if err := h.projectService.UpdateProjectIncludeFile(ctx, input.ProjectID, input.Body.RelativePath, input.Body.Content); err != nil {
		return nil, huma.Error400BadRequest((&common.ProjectUpdateError{Err: err}).Error())
	}

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.ProjectDetailsError{Err: err}).Error())
	}

	return &UpdateProjectIncludeOutput{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

// RestartProject restarts all containers in a project.
func (h *ProjectHandler) RestartProject(ctx context.Context, input *RestartProjectInput) (*RestartProjectOutput, error) {
	if h.projectService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest((&common.ProjectIDRequiredError{}).Error())
	}

	user, exists := humamw.GetCurrentUserFromContext(ctx)
	if !exists {
		return nil, huma.Error401Unauthorized((&common.NotAuthenticatedError{}).Error())
	}

	if err := h.projectService.RestartProject(ctx, input.ProjectID, *user); err != nil {
		return nil, huma.Error400BadRequest((&common.ProjectRestartError{Err: err}).Error())
	}

	return &RestartProjectOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Project restarted successfully",
			},
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

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) { //nolint:contextcheck // context is obtained from humaCtx.Context()
			humaCtx.SetHeader("Content-Type", "application/x-json-stream")
			humaCtx.SetHeader("Cache-Control", "no-cache")
			humaCtx.SetHeader("Connection", "keep-alive")
			humaCtx.SetHeader("X-Accel-Buffering", "no")

			writer := humaCtx.BodyWriter()

			_, _ = writer.Write([]byte(`{"status":"starting project image pull"}` + "\n"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}

			if err := h.projectService.PullProjectImages(humaCtx.Context(), input.ProjectID, writer, nil); err != nil {
				_, _ = fmt.Fprintf(writer, `{"error":%q}`+"\n", err.Error())
				return
			}

			_, _ = writer.Write([]byte(`{"status":"complete"}` + "\n"))
		},
	}, nil
}
