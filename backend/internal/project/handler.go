package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/project"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/samber/mo"
	"go.getarcane.app/docker/types"
	kit "go.getarcane.app/kit/pkg"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project/children/tags"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/workspace"
)

// ProjectHandler provides Huma-based project management endpoints.
type ProjectHandler struct {
	projectService  *ProjectService
	activityService *activity.ActivityService
	appCtx          context.Context
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
	Tags          string `query:"tags" doc:"Filter by tag names (comma-separated, OR semantics)"`
	Label         string `query:"label" doc:"Filter by container label key or key=value on any running service"`
	Name          string `query:"name" doc:"Filter by exact project name (comma-separated for several)"`
}

type GetProjectStatusCountsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// ListProjectTagsInput identifies the environment whose tag catalog is requested.
type ListProjectTagsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// ListProjectReferencesInput identifies the environment whose project references are requested.
type ListProjectReferencesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

// UpdateProjectTagInput identifies a project and the UI tag mutation to apply.
type UpdateProjectTagInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          project.UpdateTag
}

type DeployProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.DeployOptions
}

type DownProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type CreateProjectInput struct {
	EnvironmentID string         `path:"id" doc:"Environment ID"`
	RawBody       multipart.Form `contentType:"multipart/form-data"`
}

type GetProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type RedeployProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.DeployOptions
}

type DestroyProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.Destroy
}

type UpdateProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          project.UpdateProject
}

type RestartProjectInput struct {
	EnvironmentID string   `path:"id" doc:"Environment ID"`
	ProjectID     string   `path:"projectId" doc:"Project ID"`
	Services      []string `query:"services" doc:"Service names to restart; empty restarts all services"`
}

type UpdateProjectServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.UpdateServicesRequest
}

type ArchiveProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type UnarchiveProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type PullProjectImagesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
}

type BuildProjectInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ProjectID     string `path:"projectId" doc:"Project ID"`
	Body          *project.BuildRequest
}

// ListProjects returns a paginated list of projects.
func (h *ProjectHandler) ListProjects(ctx context.Context, input *ListProjectsInput) (*handlerutil.Page[project.Details], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if input.Status != "" {
		params.Filters["status"] = input.Status
	}
	if input.Updates != "" {
		params.Filters["updates"] = input.Updates
	}
	if input.Archived != "" {
		params.Filters["archived"] = input.Archived
	}
	if input.Tags != "" {
		params.Filters["tags"] = input.Tags
	}
	if input.Label != "" {
		params.Filters["label"] = input.Label
	}
	if input.Name != "" {
		params.Filters["name"] = input.Name
	}
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.projectService.AggregateProjects(ctx, params)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list projects: " + err.Error())
		}
		return &handlerutil.Page[project.Details]{Body: body}, nil
	}

	projects, paginationResp, err := h.projectService.ListProjects(ctx, params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, huma.Error500InternalServerError("Request was canceled")
		}
		return nil, huma.Error500InternalServerError("Failed to list projects: " + err.Error())
	}

	return &handlerutil.Page[project.Details]{
		Body: base.Paginated[project.Details]{
			Success:    true,
			Data:       kit.Ternary(projects == nil, []project.Details{}, projects),
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetProjectStatusCounts returns counts of projects by status.
func (h *ProjectHandler) GetProjectStatusCounts(ctx context.Context, input *GetProjectStatusCountsInput) (*handlerutil.Out[project.StatusCounts], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.projectService.AggregateProjectCounts(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get project status counts: " + err.Error())
		}
		return &handlerutil.Out[project.StatusCounts]{Body: body}, nil
	}
	counts, err := h.projectService.GetProjectStatusCounts(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get project status counts: " + err.Error())
	}

	return &handlerutil.Out[project.StatusCounts]{
		Body: base.ApiResponse[project.StatusCounts]{
			Success: true,
			Data:    counts,
		},
	}, nil
}

// ListProjectTags returns the reusable project tag catalog for an environment.
func (h *ProjectHandler) ListProjectTags(ctx context.Context, _ *ListProjectTagsInput) (*handlerutil.Out[[]project.TagOption], error) {
	if _, ok := httpx.EnvironmentScopeFromContext(ctx); ok {
		body, err := h.projectService.AggregateProjectTags(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list project tags: " + err.Error())
		}
		return &handlerutil.Out[[]project.TagOption]{Body: body}, nil
	}
	options, err := h.projectService.ListProjectTagOptions(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list project tags: " + err.Error())
	}
	return &handlerutil.Out[[]project.TagOption]{Body: base.ApiResponse[[]project.TagOption]{Success: true, Data: kit.Ternary(options == nil, []project.TagOption{}, options)}}, nil
}

// ListProjectReferences returns the ID and name of every project in an environment.
func (h *ProjectHandler) ListProjectReferences(ctx context.Context, _ *ListProjectReferencesInput) (*handlerutil.Out[[]project.Reference], error) {
	references, err := h.projectService.ListProjectReferences(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list project references: " + err.Error())
	}
	return &handlerutil.Out[[]project.Reference]{Body: base.ApiResponse[[]project.Reference]{Success: true, Data: references}}, nil
}

// UpdateProjectTag applies one UI-managed project tag association change.
func (h *ProjectHandler) UpdateProjectTag(ctx context.Context, input *UpdateProjectTagInput) (*handlerutil.Out[project.UpdateTagResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	var projectTags []project.Tag
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "project",
		ResourceID:     input.ProjectID,
		ResourceName:   h.projectActivityName(runtimeCtx, input.ProjectID),
		User:           user,
		Step:           "Updating project tags",
		Message:        "Updating project tags",
		SuccessMessage: "Project tags updated",
		Metadata:       database.JSON{"action": "update_tags", "tag": input.Body.Name, "attached": input.Body.Attached},
	}, func(runtimeCtx context.Context) error {
		var updateErr error
		projectTags, updateErr = h.projectService.UpdateProjectTag(runtimeCtx, input.ProjectID, input.Body.Name, input.Body.Color, input.Body.Attached, *user)
		return updateErr
	})
	if err != nil {
		switch {
		case errors.Is(err, tags.ErrComposeTagReadOnly):
			return nil, huma.Error409Conflict(err.Error())
		case errors.Is(err, gorm.ErrRecordNotFound):
			return nil, huma.Error404NotFound("Project not found")
		default:
			return nil, huma.Error400BadRequest(err.Error())
		}
	}

	return &handlerutil.Out[project.UpdateTagResponse]{Body: base.ApiResponse[project.UpdateTagResponse]{
		Success: true,
		Data: project.UpdateTagResponse{
			Tags:       projectTags,
			ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
		},
	}}, nil
}

// DeployProject deploys a Docker Compose project.
// projectStreamOperationConfig describes one streamed project
// operation (deploy, redeploy, pull, build): the activity it records and the
// action whose raw docker CLI output is streamed to the client.
type projectStreamOperationConfig struct {
	ActivityType   activitytypes.Type
	Step           string
	StartMessage   string
	WriterStep     string
	FailureMessage string
	SuccessMessage string
	Metadata       database.JSON
	// Action runs the operation. ctx carries the stream writer under
	// types.ProgressWriterKey; it is also passed directly for actions
	// that take a writer parameter.
	Action func(ctx context.Context, writer io.Writer) error
}

// projectActivityName returns the project's name for activity records, falling back to its ID.
func (h *ProjectHandler) projectActivityName(ctx context.Context, projectID string) string {
	proj, err := h.projectService.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil || proj.Name == "" {
		return projectID
	}
	return proj.Name
}

// streamProjectOperation is the shared scaffold for the streamed
// project endpoints: NDJSON headers, activity lifecycle (started frame, queue
// slot, completion), the activity-teeing writer, and the terminal done/error
// frames.
func (h *ProjectHandler) streamProjectOperation(environmentID, projectID string, user *usertypes.Actor, cfg projectStreamOperationConfig) *huma.StreamResponse {
	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			activityID, runtimeCtx := activitylib.StartHandlerActivity(
				runtimeCtx,
				h.activityService,
				environmentID,
				cfg.ActivityType,
				"project",
				projectID,
				h.projectActivityName(runtimeCtx, projectID),
				user,
				cfg.Step,
				cfg.StartMessage,
				kit.Ternary(cfg.Metadata == nil, database.JSON{"projectID": projectID}, cfg.Metadata),
				true,
			)
			activitylib.WriteStartedLine(rawWriter, activityID)
			if f, ok := rawWriter.(http.Flusher); ok {
				f.Flush()
			}
			activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, environmentID)

			writer := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, rawWriter, cfg.WriterStep)

			opCtx := context.WithValue(runtimeCtx, types.ProgressWriterKey{}, writer)
			if err := cfg.Action(opCtx, writer); err != nil {
				activitylib.FlushWriter(writer)
				activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.FailureMessage, err)
				_, _ = fmt.Fprintf(writer, "{\"error\":%q}\n", err.Error())
				if f, ok := writer.(http.Flusher); ok {
					f.Flush()
				}
				return
			}

			activitylib.FlushWriter(writer)
			activitylib.WriteDoneLine(rawWriter)
			activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.SuccessMessage, nil)
		},
	}
}

func (h *ProjectHandler) DeployProject(ctx context.Context, input *DeployProjectInput) (*huma.StreamResponse, error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	//nolint:contextcheck // the stream body runs on humaCtx.Context(), not the handler ctx
	return h.streamProjectOperation(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfig{
		ActivityType:   activitytypes.TypeProjectDeploy,
		Step:           "Starting deployment",
		StartMessage:   "Project deployment started",
		WriterStep:     "Deploying project",
		FailureMessage: "Project deployment failed",
		SuccessMessage: "Project deployment completed",
		Action: func(opCtx context.Context, _ io.Writer) error {
			return h.projectService.DeployProject(opCtx, input.ProjectID, *user, input.Body)
		},
	}), nil
}

// DownProject brings down a Docker Compose project.
func (h *ProjectHandler) DownProject(ctx context.Context, input *DownProjectInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runProjectActivityAction(ctx, input.EnvironmentID, input.ProjectID, projectActivityActionConfig{
		ActivityType:    activitytypes.TypeProjectDown,
		Step:            "Stopping project",
		StartMessage:    "Project stop requested",
		WriterStep:      "Stopping project",
		FailureMessage:  "Project stopped",
		SuccessComplete: "Project stopped",
		SuccessMessage:  "Project brought down successfully",
		Action: func(runtimeCtx context.Context, projectID string, user usertypes.Actor) error {
			return h.projectService.DownProject(runtimeCtx, projectID, user)
		},
		Error: func(err error) error {
			if errors.Is(err, common.ErrProjectArchived) || errors.Is(err, common.ErrProjectEnvUnreadable) {
				return huma.Error400BadRequest(err.Error())
			}
			return huma.Error500InternalServerError("Failed to bring down project: " + err.Error())
		},
	})
}

// CreateProject creates a new Docker Compose project.
func (h *ProjectHandler) CreateProject(ctx context.Context, input *CreateProjectInput) (*handlerutil.Out[project.CreateReponse], error) {
	projectInput, err := handlerutil.ParseMultipartJSONPart[project.CreateProject](input.RawBody, "project")
	if err != nil {
		return nil, err
	}
	manifest, err := handlerutil.ParseMultipartJSONPart[project.CreateProjectWorkspaceManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, workspace.MaxFileSizeBytes(h.projectService.config.ProjectWorkspaceMaxFileSizeMB))
	if err != nil {
		return nil, err
	}
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	var proj *Project
	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "project",
		ResourceID:     projectInput.Name,
		ResourceName:   projectInput.Name,
		User:           user,
		Step:           "Creating project",
		Message:        "Creating project",
		SuccessMessage: "Project created successfully",
		Metadata:       database.JSON{"action": "create_project"},
	}, func(runtimeCtx context.Context) error {
		var createErr error
		proj, createErr = h.projectService.CreateProject(
			runtimeCtx,
			projectInput.Name,
			projectInput.ComposeContent,
			projectInput.EnvContent,
			manifest,
			uploads,
			projectInput.Tags,
			projectInput.TagColors,
			*user,
		)
		return createErr
	})
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error500InternalServerError("Failed to create project: " + err.Error())
	}

	projectTags, err := h.projectService.GetProjectTags(ctx, proj.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load project tags: " + err.Error())
	}

	return &handlerutil.Out[project.CreateReponse]{
		Body: base.ApiResponse[project.CreateReponse]{
			Success: true,
			Data: project.CreateReponse{
				ID:              proj.ID,
				Name:            proj.Name,
				DirName:         kit.FromPtr(proj.DirName),
				RelativePath:    h.projectService.GetProjectRelativePath(ctx, proj.Path),
				Path:            proj.Path,
				Status:          string(proj.Status),
				StatusReason:    proj.StatusReason,
				ServiceCount:    proj.ServiceCount,
				RunningCount:    proj.RunningCount,
				GitOpsManagedBy: proj.GitOpsManagedBy,
				IsArchived:      proj.IsArchived,
				ArchivedAt:      proj.ArchivedAt,
				CreatedAt:       proj.CreatedAt.Format(time.RFC3339),
				UpdatedAt:       proj.UpdatedAt.Format(time.RFC3339),
				ActivityID:      mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
				Tags:            projectTags,
			},
		},
	}, nil
}

// GetProject returns a project by ID.
func (h *ProjectHandler) GetProject(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	return h.getProjectDetailsWithOptions(ctx, input, project.DetailsOptions{})
}

func (h *ProjectHandler) getProjectDetailsWithOptions(ctx context.Context, input *GetProjectInput, opts project.DetailsOptions) (*handlerutil.Out[project.Details], error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID, opts)
	if err != nil {
		return nil, huma.Error404NotFound("Failed to get project details: " + err.Error())
	}

	return &handlerutil.Out[project.Details]{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

func (h *ProjectHandler) GetProjectCompose(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	return h.getProjectDetailsWithOptions(ctx, input, project.DetailsOptions{
		IncludeComposeContent: true,
		IncludeEnvState:       true,
		IncludeIncludeFiles:   true,
		IncludeServiceConfigs: true,
	})
}

func (h *ProjectHandler) GetProjectRuntime(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	return h.getProjectDetailsWithOptions(ctx, input, project.DetailsOptions{
		IncludeRuntimeServices: true,
	})
}

func (h *ProjectHandler) GetProjectUpdates(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	return h.getProjectDetailsWithOptions(ctx, input, project.DetailsOptions{
		IncludeServiceConfigs: true,
		IncludeUpdateInfo:     true,
	})
}

// RedeployProject redeploys a Docker Compose project.
// RedeployProject pulls project images and re-deploys, streaming the raw
// docker CLI output as NDJSON like DeployProject.
func (h *ProjectHandler) RedeployProject(ctx context.Context, input *RedeployProjectInput) (*huma.StreamResponse, error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	//nolint:contextcheck // the stream body runs on humaCtx.Context(), not the handler ctx
	return h.streamProjectOperation(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfig{
		ActivityType:   activitytypes.TypeProjectRedeploy,
		Step:           "Starting redeploy",
		StartMessage:   "Project redeploy started",
		WriterStep:     "Redeploying project",
		FailureMessage: "Project redeploy failed",
		SuccessMessage: "Project redeploy completed",
		Action: func(opCtx context.Context, _ io.Writer) error {
			return h.projectService.RedeployProject(opCtx, input.ProjectID, *user, input.Body)
		},
	}), nil
}

// DestroyProject destroys a Docker Compose project.
func (h *ProjectHandler) DestroyProject(ctx context.Context, input *DestroyProjectInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runProjectActivityAction(ctx, input.EnvironmentID, input.ProjectID, projectActivityActionConfig{
		ActivityType:    activitytypes.TypeProjectDestroy,
		Step:            "Destroying project",
		StartMessage:    "Project destroy requested",
		WriterStep:      "Destroying project",
		FailureMessage:  "Project destroyed",
		SuccessComplete: "Project destroyed",
		SuccessMessage:  "Project destroyed successfully",
		Metadata: database.JSON{
			"projectID":     input.ProjectID,
			"removeFiles":   mo.PointerToOption(kit.FromPtr(input.Body).RemoveFiles).OrElse(true),
			"removeVolumes": kit.FromPtr(input.Body).RemoveVolumes,
		},
		Action: func(runtimeCtx context.Context, projectID string, user usertypes.Actor) error {
			return h.projectService.DestroyProject(runtimeCtx, projectID, mo.PointerToOption(kit.FromPtr(input.Body).RemoveFiles).OrElse(true), kit.FromPtr(input.Body).RemoveVolumes, user)
		},
		Error: func(err error) error {
			return huma.Error500InternalServerError("Failed to destroy project: " + err.Error())
		},
	})
}

// UpdateProject updates a Docker Compose project.
func (h *ProjectHandler) UpdateProject(ctx context.Context, input *UpdateProjectInput) (*handlerutil.Out[project.Details], error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, err := activitylib.RunHandlerActivity(runtimeCtx, h.activityService, activitylib.HandlerOptions{
		EnvironmentID:  input.EnvironmentID,
		Type:           activitytypes.TypeResourceAction,
		ResourceType:   "project",
		ResourceID:     input.ProjectID,
		ResourceName:   mo.PointerToOption(input.Body.Name).OrEmpty(),
		User:           user,
		Step:           "Updating project",
		Message:        "Updating project",
		SuccessMessage: "Project updated successfully",
		Metadata:       database.JSON{"action": "update_project", "projectID": input.ProjectID},
	}, func(runtimeCtx context.Context) error {
		_, updateErr := h.projectService.UpdateProject(runtimeCtx, input.ProjectID, input.Body.Name, input.Body.ComposeContent, input.Body.EnvContent, input.Body.OverrideContent, *user)
		return updateErr
	})
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error400BadRequest("Failed to update project: " + err.Error())
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
		return nil, huma.Error500InternalServerError("Failed to get project details: " + err.Error())
	}
	return &handlerutil.Out[project.Details]{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data: project.Details{ //exhaustruct:enforce
				EnvironmentTag:     details.EnvironmentTag,
				Tags:               details.Tags,
				StatusReason:       details.StatusReason,
				ActivityID:         mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
				LastSyncCommit:     details.LastSyncCommit,
				GitOpsManagedBy:    details.GitOpsManagedBy,
				UpdateInfo:         details.UpdateInfo,
				ArchivedAt:         details.ArchivedAt,
				Status:             details.Status,
				DirName:            details.DirName,
				ComposeContent:     details.ComposeContent,
				ComposeFileName:    details.ComposeFileName,
				ComposeFiles:       details.ComposeFiles,
				EnvContent:         details.EnvContent,
				OverrideContent:    details.OverrideContent,
				OverrideFileName:   details.OverrideFileName,
				Name:               details.Name,
				GitRepositoryURL:   details.GitRepositoryURL,
				IconLightURL:       details.IconLightURL,
				RelativePath:       details.RelativePath,
				ID:                 details.ID,
				IconDarkURL:        details.IconDarkURL,
				Path:               details.Path,
				UpdatedAt:          details.UpdatedAt,
				CreatedAt:          details.CreatedAt,
				RuntimeServices:    details.RuntimeServices,
				Services:           details.Services,
				URLs:               details.URLs,
				IncludeFiles:       details.IncludeFiles,
				RunningCount:       details.RunningCount,
				ServiceCount:       details.ServiceCount,
				IsDiscovered:       details.IsDiscovered,
				IsArchived:         details.IsArchived,
				HasBuildDirective:  details.HasBuildDirective,
				RedeployDisabled:   details.RedeployDisabled,
				ConfigurationError: details.ConfigurationError,
			},
		},
	}, nil
}

// RestartProject restarts the given services in a project (all services when none are specified).
func (h *ProjectHandler) RestartProject(ctx context.Context, input *RestartProjectInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runProjectActivityAction(ctx, input.EnvironmentID, input.ProjectID, projectActivityActionConfig{
		ActivityType:    activitytypes.TypeProjectRestart,
		Step:            "Restarting project",
		StartMessage:    "Project restart requested",
		WriterStep:      "Restarting project",
		FailureMessage:  "Project restarted",
		SuccessComplete: "Project restarted",
		SuccessMessage:  "Project restarted successfully",
		Action: func(runtimeCtx context.Context, projectID string, user usertypes.Actor) error {
			return h.projectService.RestartProject(runtimeCtx, projectID, input.Services, user)
		},
		Error: func(err error) error {
			if errors.Is(err, common.ErrProjectArchived) {
				return huma.Error400BadRequest(err.Error())
			}
			return huma.Error400BadRequest("Failed to restart project: " + err.Error())
		},
	})
}

// UpdateProjectServices pulls the latest images for the given services and recreates them.
func (h *ProjectHandler) UpdateProjectServices(ctx context.Context, input *UpdateProjectServicesInput) (*handlerutil.Out[base.MessageResponse], error) {
	return h.runProjectActivityAction(ctx, input.EnvironmentID, input.ProjectID, projectActivityActionConfig{
		ActivityType:    activitytypes.TypeAutoUpdate,
		Step:            "Updating project services",
		StartMessage:    "Project services update requested",
		WriterStep:      "Updating project services",
		FailureMessage:  "Project services update failed",
		SuccessComplete: "Project services updated",
		SuccessMessage:  "Project services updated successfully",
		Queue:           true,
		Action: func(runtimeCtx context.Context, projectID string, user usertypes.Actor) error {
			return h.projectService.UpdateProjectServices(runtimeCtx, projectID, kit.FromPtr(input.Body).Services, user, true)
		},
		Error: func(err error) error {
			if errors.Is(err, common.ErrProjectArchived) {
				return huma.Error400BadRequest(err.Error())
			}
			return huma.Error400BadRequest("Failed to update project: " + err.Error())
		},
	})
}

type projectActivityActionConfig struct {
	ActivityType    activitytypes.Type
	Step            string
	StartMessage    string
	WriterStep      string
	FailureMessage  string
	SuccessComplete string
	SuccessMessage  string
	// Queue routes the activity through the per-environment concurrency
	// limiter; set for long-running deploy-like actions, not quick restarts.
	Queue    bool
	Metadata database.JSON
	Action   func(context.Context, string, usertypes.Actor) error
	Error    func(error) error
}

func (h *ProjectHandler) runProjectActivityAction(ctx context.Context, environmentID, projectID string, cfg projectActivityActionConfig) (*handlerutil.Out[base.MessageResponse], error) {
	if projectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		environmentID,
		cfg.ActivityType,
		"project",
		projectID,
		h.projectActivityName(runtimeCtx, projectID),
		user,
		cfg.Step,
		cfg.StartMessage,
		kit.Ternary(cfg.Metadata == nil, database.JSON{"projectID": projectID}, cfg.Metadata),
		cfg.Queue,
	)
	if cfg.Queue {
		activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, environmentID)
	}
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, cfg.WriterStep)
	if actionErr := cfg.Action(context.WithValue(runtimeCtx, types.ProgressWriterKey{}, activityWriter), projectID, *user); actionErr != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.FailureMessage, actionErr)
		return nil, cfg.Error(actionErr)
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.SuccessComplete, nil)
	return handlerutil.MessageOutput(cfg.SuccessMessage, activityID), nil
}

func (h *ProjectHandler) ArchiveProject(ctx context.Context, input *ArchiveProjectInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if archiveProjectErr := h.projectService.ArchiveProject(ctx, input.ProjectID, *user); archiveProjectErr != nil {
		if errors.Is(archiveProjectErr, common.ErrProjectMustBeStopped) {
			return nil, huma.Error400BadRequest(archiveProjectErr.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to archive project: " + archiveProjectErr.Error())
	}

	return handlerutil.MessageOutput("Project archived successfully", ""), nil
}

func (h *ProjectHandler) UnarchiveProject(ctx context.Context, input *UnarchiveProjectInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	if unarchiveProjectErr := h.projectService.UnarchiveProject(ctx, input.ProjectID, *user); unarchiveProjectErr != nil {
		return nil, huma.Error500InternalServerError("Failed to unarchive project: " + unarchiveProjectErr.Error())
	}

	return handlerutil.MessageOutput("Project unarchived successfully", ""), nil
}

// PullProjectImages pulls all images for a project with streaming progress.
func (h *ProjectHandler) PullProjectImages(ctx context.Context, input *PullProjectImagesInput) (*huma.StreamResponse, error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	//nolint:contextcheck // the stream body runs on humaCtx.Context(), not the handler ctx
	return h.streamProjectOperation(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfig{
		ActivityType:   activitytypes.TypeProjectPull,
		Step:           "Pulling project images",
		StartMessage:   "Project image pull started",
		WriterStep:     "Pulling project images",
		FailureMessage: "Project image pull failed",
		SuccessMessage: "Project image pull completed",
		Action: func(opCtx context.Context, writer io.Writer) error {
			return h.projectService.PullProjectImages(opCtx, input.ProjectID, writer, *user, nil)
		},
	}), nil
}

// BuildProjectImages builds compose services with build directives.
func (h *ProjectHandler) BuildProjectImages(ctx context.Context, input *BuildProjectInput) (*huma.StreamResponse, error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	options := project.BuildOptions{
		Services: kit.FromPtr(input.Body).Services,
		Provider: kit.FromPtr(input.Body).Provider,
		Push:     kit.FromPtr(input.Body).Push,
		Load:     kit.FromPtr(input.Body).Load,
	}

	//nolint:contextcheck // the stream body runs on humaCtx.Context(), not the handler ctx
	return h.streamProjectOperation(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfig{
		ActivityType:   activitytypes.TypeProjectBuild,
		Step:           "Building project images",
		StartMessage:   "Project image build started",
		WriterStep:     "Building project images",
		FailureMessage: "Project image build failed",
		SuccessMessage: "Project image build completed",
		Metadata:       database.JSON{"projectID": input.ProjectID, "services": options.Services},
		Action: func(opCtx context.Context, writer io.Writer) error {
			return h.projectService.BuildProjectServices(opCtx, input.ProjectID, options, writer, user)
		},
	}), nil
}

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

func (h *ProjectHandler) GetProjectWorkspace(ctx context.Context, input *GetProjectWorkspaceInput) (*handlerutil.Out[workspacetypes.Workspace], error) {
	result, err := h.projectService.GetProjectWorkspace(ctx, input.ProjectID)
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error500InternalServerError("internal error")
	}
	return &handlerutil.Out[workspacetypes.Workspace]{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: *result}}, nil
}

func (h *ProjectHandler) GetProjectWorkspaceFile(ctx context.Context, input *GetProjectWorkspaceFileInput) (*handlerutil.Out[workspacetypes.FileContent], error) {
	result, err := h.projectService.GetProjectWorkspaceFile(ctx, input.ProjectID, input.RelativePath)
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error500InternalServerError("internal error")
	}
	return &handlerutil.Out[workspacetypes.FileContent]{Body: base.ApiResponse[workspacetypes.FileContent]{Success: true, Data: *result}}, nil
}

func (h *ProjectHandler) DownloadProjectWorkspaceFile(ctx context.Context, input *GetProjectWorkspaceFileInput) (*huma.StreamResponse, error) {
	reader, size, name, err := h.projectService.DownloadProjectWorkspaceFile(ctx, input.ProjectID, input.RelativePath)
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error500InternalServerError("internal error")
	}
	return handlerutil.DownloadResponse(reader, size, name), nil
}

func (h *ProjectHandler) UpdateProjectWorkspace(ctx context.Context, input *UpdateProjectWorkspaceInput) (*handlerutil.Out[workspacetypes.Workspace], error) {
	manifest, err := handlerutil.ParseMultipartJSONPart[project.WorkspaceUpdateManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, workspace.MaxFileSizeBytes(h.projectService.config.ProjectWorkspaceMaxFileSizeMB))
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
		EnvironmentID: input.EnvironmentID, Type: activitytypes.TypeResourceAction, ResourceType: "project", ResourceID: input.ProjectID, ResourceName: h.projectActivityName(
			runtimeCtx,
			input.ProjectID,
		), User: user,
		Step: "Updating project workspace", Message: "Updating project workspace", SuccessMessage: "Project workspace updated successfully",
		Metadata: database.JSON{"action": "update_project_workspace", "fileChangeCount": len(manifest.FileChanges)},
	}, func(runtimeCtx context.Context) error {
		var updateErr error
		result, updateErr = h.projectService.UpdateProjectWorkspace(runtimeCtx, input.ProjectID, manifest, uploads, *user)
		return updateErr
	})
	if err != nil {
		if apiErr := common.ToAPIError(err); apiErr.HTTPStatus() != http.StatusInternalServerError {
			return nil, huma.NewError(apiErr.HTTPStatus(), apiErr.Message)
		}
		return nil, huma.Error500InternalServerError("internal error")
	}
	return &handlerutil.Out[workspacetypes.Workspace]{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: workspacetypes.Workspace{
		Files:             result.Files,
		FileTreeRevision:  result.FileTreeRevision,
		FileTreeTruncated: result.FileTreeTruncated,
		ActivityID:        mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
	}}}, nil
}
