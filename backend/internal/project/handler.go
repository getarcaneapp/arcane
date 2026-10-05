package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	activitytypes "github.com/getarcaneapp/arcane/types/v2/activity"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/project"
	usertypes "github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/getarcaneapp/arcane/types/v2/volume"
	workspacetypes "github.com/getarcaneapp/arcane/types/v2/workspace"
	"github.com/samber/mo"
	"go.getarcane.app/docker/types"
	"go.getarcane.app/kit/pkg/mapping"
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
	Body          *struct {
		Services []string `json:"services,omitempty" doc:"Service names to update; empty updates all services"`
	}
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
	Body          *struct {
		Services []string `json:"services,omitempty" doc:"Service names to build"`
		Provider string   `json:"provider,omitempty" doc:"Build provider override"`
		Push     *bool    `json:"push,omitempty" doc:"Push images"`
		Load     *bool    `json:"load,omitempty" doc:"Load images into Docker"`
	}
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

	projects, paginationResp, err := h.projectService.ListProjects(ctx, params)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, huma.Error500InternalServerError("Request was canceled")
		}
		return nil, huma.Error500InternalServerError("Failed to list projects: " + err.Error())
	}

	if projects == nil {
		projects = []project.Details{}
	}

	return &handlerutil.Page[project.Details]{
		Body: base.Paginated[project.Details]{
			Success:    true,
			Data:       projects,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetProjectStatusCounts returns counts of projects by status.
func (h *ProjectHandler) GetProjectStatusCounts(ctx context.Context, input *GetProjectStatusCountsInput) (*handlerutil.Out[project.StatusCounts], error) {
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
	options, err := h.projectService.ListProjectTagOptions(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list project tags: " + err.Error())
	}
	if options == nil {
		options = []project.TagOption{}
	}
	return &handlerutil.Out[[]project.TagOption]{Body: base.ApiResponse[[]project.TagOption]{Success: true, Data: options}}, nil
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
		ResourceName:   h.projectActivityNameInternal(runtimeCtx, input.ProjectID),
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
// projectStreamOperationConfigInternal describes one streamed project
// operation (deploy, redeploy, pull, build): the activity it records and the
// action whose raw docker CLI output is streamed to the client.
type projectStreamOperationConfigInternal struct {
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

// projectActivityNameInternal returns the project's name for activity records, falling back to its ID.
func (h *ProjectHandler) projectActivityNameInternal(ctx context.Context, projectID string) string {
	proj, err := h.projectService.GetProjectFromDatabaseByID(ctx, projectID)
	if err != nil || proj.Name == "" {
		return projectID
	}
	return proj.Name
}

// streamProjectOperationInternal is the shared scaffold for the streamed
// project endpoints: NDJSON headers, activity lifecycle (started frame, queue
// slot, completion), the activity-teeing writer, and the terminal done/error
// frames.
func (h *ProjectHandler) streamProjectOperationInternal(environmentID, projectID string, user *usertypes.Actor, cfg projectStreamOperationConfigInternal) *huma.StreamResponse {
	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			httpx.SetJSONStreamHeaders(humaCtx)

			runtimeCtx := utils.ActivityRuntimeContext(humaCtx.Context(), h.appCtx)
			rawWriter := humaCtx.BodyWriter()
			metadata := cfg.Metadata
			if metadata == nil {
				metadata = database.JSON{"projectID": projectID}
			}
			activityID, runtimeCtx := activitylib.StartHandlerActivity(
				runtimeCtx,
				h.activityService,
				environmentID,
				cfg.ActivityType,
				"project",
				projectID,
				h.projectActivityNameInternal(runtimeCtx, projectID),
				user,
				cfg.Step,
				cfg.StartMessage,
				metadata,
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
	return h.streamProjectOperationInternal(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfigInternal{
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
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		activitytypes.TypeProjectDown,
		"project",
		input.ProjectID,
		h.projectActivityNameInternal(
			runtimeCtx,
			input.ProjectID,
		),
		user,
		"Stopping project",
		"Project stop requested",
		database.JSON{
			"projectID": input.ProjectID,
		},
		false,
	)
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Stopping project")
	downCtx := context.WithValue(runtimeCtx, types.ProgressWriterKey{}, activityWriter)
	if downProjectErr := h.projectService.DownProject(downCtx, input.ProjectID, *user); downProjectErr != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project stopped", downProjectErr)
		if errors.Is(downProjectErr, common.ErrProjectArchived) || errors.Is(downProjectErr, common.ErrProjectEnvUnreadable) {
			return nil, huma.Error400BadRequest(downProjectErr.Error())
		}
		return nil, huma.Error500InternalServerError("Failed to bring down project: " + downProjectErr.Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project stopped", nil)

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message:    "Project brought down successfully",
				ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
}

func projectUpdateHTTPErrorInternal(err error) error {
	if conflictErr, ok := errors.AsType[*volume.ProjectVolumeRenameConflictError](err); ok {
		return huma.Error409Conflict(conflictErr.Error())
	}
	if inUseErr, ok := errors.AsType[*volume.ProjectVolumeRenameInUseError](err); ok {
		return huma.Error409Conflict(inUseErr.Error())
	}
	if spaceErr, ok := errors.AsType[*volume.ProjectVolumeRenameInsufficientSpaceError](err); ok {
		return huma.NewError(http.StatusInsufficientStorage, spaceErr.Error())
	}
	return projectWorkspaceRequestHTTPErrorInternal(err)
}

// projectWorkspaceRequestHTTPErrorInternal maps workspace validation errors that
// can also surface during atomic project creation or configuration updates.
func projectWorkspaceRequestHTTPErrorInternal(err error) error {
	if errors.Is(err, common.ErrProjectWorkspaceConflict) {
		return huma.Error409Conflict(err.Error())
	}
	if errors.Is(err, common.ErrProjectWorkspaceForbidden) {
		return huma.Error403Forbidden(err.Error())
	}
	if errors.Is(err, common.ErrProjectWorkspaceBadRequest) {
		return huma.Error400BadRequest(err.Error())
	}
	return nil
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
	maxFileSizeMB := workspace.DefaultMaxFileSizeMB
	if h.projectService != nil && h.projectService.config != nil {
		maxFileSizeMB = h.projectService.config.ProjectWorkspaceMaxFileSizeMB
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, workspace.MaxFileSizeBytes(maxFileSizeMB))
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
		if httpErr := projectWorkspaceRequestHTTPErrorInternal(err); httpErr != nil {
			return nil, httpErr
		}
		return nil, huma.Error500InternalServerError("Failed to create project: " + err.Error())
	}

	var response project.CreateReponse
	if mapStructErr := mapping.MapStruct(proj, &response); mapStructErr != nil {
		return nil, huma.Error500InternalServerError("failed to map response")
	}
	response.Status = string(proj.Status)
	response.StatusReason = proj.StatusReason
	response.CreatedAt = proj.CreatedAt.Format(time.RFC3339)
	response.UpdatedAt = proj.UpdatedAt.Format(time.RFC3339)
	response.DirName = mo.PointerToOption(proj.DirName).OrEmpty()
	response.RelativePath = h.projectService.GetProjectRelativePath(ctx, proj.Path)
	response.GitOpsManagedBy = proj.GitOpsManagedBy
	response.IsArchived = proj.IsArchived
	response.ArchivedAt = proj.ArchivedAt
	response.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	response.Tags, err = h.projectService.GetProjectTags(ctx, proj.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to load project tags: " + err.Error())
	}

	return &handlerutil.Out[project.CreateReponse]{
		Body: base.ApiResponse[project.CreateReponse]{
			Success: true,
			Data:    response,
		},
	}, nil
}

// GetProject returns a project by ID.
func (h *ProjectHandler) GetProject(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	if input.ProjectID == "" {
		return nil, huma.Error400BadRequest("Project ID is required")
	}

	details, err := h.projectService.GetProjectDetails(ctx, input.ProjectID, project.DetailsOptions{})
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

func (h *ProjectHandler) getProjectDetailsWithOptionsInternal(ctx context.Context, input *GetProjectInput, opts project.DetailsOptions) (*handlerutil.Out[project.Details], error) {
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
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
		IncludeComposeContent: true,
		IncludeEnvState:       true,
		IncludeIncludeFiles:   true,
		IncludeServiceConfigs: true,
	})
}

func (h *ProjectHandler) GetProjectRuntime(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
		IncludeRuntimeServices: true,
	})
}

func (h *ProjectHandler) GetProjectUpdates(ctx context.Context, input *GetProjectInput) (*handlerutil.Out[project.Details], error) {
	return h.getProjectDetailsWithOptionsInternal(ctx, input, project.DetailsOptions{
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
	return h.streamProjectOperationInternal(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfigInternal{
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
	user, err := handlerutil.RequireUser(ctx)
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
			"projectId", input.ProjectID)
	} else {
		slog.DebugContext(ctx, "DestroyProject handler received nil body",
			"projectId", input.ProjectID)
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	activityID, runtimeCtx := activitylib.StartHandlerActivity(
		runtimeCtx,
		h.activityService,
		input.EnvironmentID,
		activitytypes.TypeProjectDestroy,
		"project",
		input.ProjectID,
		h.projectActivityNameInternal(
			runtimeCtx,
			input.ProjectID,
		),
		user,
		"Destroying project",
		"Project destroy requested",
		database.JSON{
			"projectID":     input.ProjectID,
			"removeFiles":   removeFiles,
			"removeVolumes": removeVolumes,
		},
		false,
	)
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, "Destroying project")
	destroyCtx := context.WithValue(runtimeCtx, types.ProgressWriterKey{}, activityWriter)
	if destroyProjectErr := h.projectService.DestroyProject(destroyCtx, input.ProjectID, removeFiles, removeVolumes, *user); destroyProjectErr != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project destroyed", destroyProjectErr)
		return nil, huma.Error500InternalServerError("Failed to destroy project: " + destroyProjectErr.Error())
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, "Project destroyed", nil)

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message:    "Project destroyed successfully",
				ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
			},
		},
	}, nil
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
		if httpErr := projectUpdateHTTPErrorInternal(err); httpErr != nil {
			return nil, httpErr
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
	details.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()

	return &handlerutil.Out[project.Details]{
		Body: base.ApiResponse[project.Details]{
			Success: true,
			Data:    details,
		},
	}, nil
}

// RestartProject restarts the given services in a project (all services when none
// are specified).
func (h *ProjectHandler) RestartProject(ctx context.Context, input *RestartProjectInput) (*handlerutil.Out[base.MessageResponse], error) {
	response, err := h.runProjectActivityActionResponseInternal(ctx, input.EnvironmentID, input.ProjectID, h.restartProjectActivityConfigInternal(input.Services))
	if err != nil {
		return nil, err
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: response,
	}, nil
}

// UpdateProjectServices pulls the latest images for the given services and recreates them.
func (h *ProjectHandler) UpdateProjectServices(ctx context.Context, input *UpdateProjectServicesInput) (*handlerutil.Out[base.MessageResponse], error) {
	var serviceNames []string
	if input.Body != nil {
		serviceNames = input.Body.Services
	}

	response, err := h.runProjectActivityActionResponseInternal(ctx, input.EnvironmentID, input.ProjectID, h.updateProjectServicesActivityConfigInternal(serviceNames))
	if err != nil {
		return nil, err
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: response,
	}, nil
}

type projectActivityActionConfigInternal struct {
	ActivityType    activitytypes.Type
	Step            string
	StartMessage    string
	WriterStep      string
	FailureMessage  string
	SuccessComplete string
	SuccessMessage  string
	// Queue routes the activity through the per-environment concurrency
	// limiter; set for long-running deploy-like actions, not quick restarts.
	Queue  bool
	Action func(context.Context, string, usertypes.Actor) error
	Error  func(error) error
}

func (h *ProjectHandler) updateProjectServicesActivityConfigInternal(services []string) projectActivityActionConfigInternal {
	return projectActivityActionConfigInternal{
		ActivityType:    activitytypes.TypeAutoUpdate,
		Step:            "Updating project services",
		StartMessage:    "Project services update requested",
		WriterStep:      "Updating project services",
		FailureMessage:  "Project services update failed",
		SuccessComplete: "Project services updated",
		SuccessMessage:  "Project services updated successfully",
		Queue:           true,
		Action: func(runtimeCtx context.Context, projectID string, user usertypes.Actor) error {
			return h.projectService.UpdateProjectServices(runtimeCtx, projectID, services, user, true)
		},
		Error: projectArchivedActionErrorInternal(func(err error) error {
			return huma.Error400BadRequest("Failed to update project: " + err.Error())
		}),
	}
}

func (h *ProjectHandler) restartProjectActivityConfigInternal(services []string) projectActivityActionConfigInternal {
	return projectActivityActionConfigInternal{
		ActivityType:    activitytypes.TypeProjectRestart,
		Step:            "Restarting project",
		StartMessage:    "Project restart requested",
		WriterStep:      "Restarting project",
		FailureMessage:  "Project restarted",
		SuccessComplete: "Project restarted",
		SuccessMessage:  "Project restarted successfully",
		Action: func(runtimeCtx context.Context, projectID string, user usertypes.Actor) error {
			return h.projectService.RestartProject(runtimeCtx, projectID, services, user)
		},
		Error: projectArchivedActionErrorInternal(func(err error) error {
			return huma.Error400BadRequest("Failed to restart project: " + err.Error())
		}),
	}
}

func projectArchivedActionErrorInternal(fallback func(error) error) func(error) error {
	return func(err error) error {
		if errors.Is(err, common.ErrProjectArchived) {
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
	if projectID == "" {
		return base.MessageResponse{}, huma.Error400BadRequest("Project ID is required")
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return base.MessageResponse{}, err
	}

	runtimeCtx := utils.ActivityRuntimeContext(ctx, h.appCtx)
	projectName := h.projectActivityNameInternal(runtimeCtx, projectID)
	var activityID string
	if cfg.Queue {
		activityID, runtimeCtx = activitylib.StartHandlerActivity(
			runtimeCtx,
			h.activityService,
			environmentID,
			cfg.ActivityType,
			"project",
			projectID,
			projectName,
			user,
			cfg.Step,
			cfg.StartMessage,
			database.JSON{
				"projectID": projectID,
			},
			true,
		)
		activitylib.AwaitHandlerActivitySlot(runtimeCtx, h.activityService, activityID, environmentID)
	} else {
		activityID, runtimeCtx = activitylib.StartHandlerActivity(
			runtimeCtx,
			h.activityService,
			environmentID,
			cfg.ActivityType,
			"project",
			projectID,
			projectName,
			user,
			cfg.Step,
			cfg.StartMessage,
			database.JSON{
				"projectID": projectID,
			},
			false,
		)
	}
	activityWriter := activitylib.NewWriter(runtimeCtx, h.activityService, activityID, io.Discard, cfg.WriterStep)
	actionCtx := context.WithValue(runtimeCtx, types.ProgressWriterKey{}, activityWriter)
	if actionErr := cfg.Action(actionCtx, projectID, *user); actionErr != nil {
		activitylib.FlushWriter(activityWriter)
		activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.FailureMessage, actionErr)
		return base.MessageResponse{}, cfg.Error(actionErr)
	}
	activitylib.FlushWriter(activityWriter)
	activitylib.CompleteHandlerActivity(runtimeCtx, h.activityService, activityID, cfg.SuccessComplete, nil)

	return base.MessageResponse{
		Message:    cfg.SuccessMessage,
		ActivityID: mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer(),
	}, nil
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

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Project archived successfully"},
		},
	}, nil
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

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data:    base.MessageResponse{Message: "Project unarchived successfully"},
		},
	}, nil
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
	return h.streamProjectOperationInternal(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfigInternal{
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

	options := project.BuildOptions{}
	if input.Body != nil {
		options.Services = input.Body.Services
		options.Provider = input.Body.Provider
		options.Push = input.Body.Push
		options.Load = input.Body.Load
	}

	//nolint:contextcheck // the stream body runs on humaCtx.Context(), not the handler ctx
	return h.streamProjectOperationInternal(input.EnvironmentID, input.ProjectID, user, projectStreamOperationConfigInternal{
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
	case errors.Is(err, common.ErrProjectWorkspaceBadRequest), errors.Is(err, common.ErrProjectEnvUnreadable):
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
	return handlerutil.DownloadResponse(reader, size, name), nil
}

func (h *ProjectHandler) UpdateProjectWorkspace(ctx context.Context, input *UpdateProjectWorkspaceInput) (*handlerutil.Out[workspacetypes.Workspace], error) {
	manifest, err := handlerutil.ParseMultipartJSONPart[project.WorkspaceUpdateManifest](input.RawBody, "manifest")
	if err != nil {
		return nil, err
	}
	maxFileSizeMB := workspace.DefaultMaxFileSizeMB
	if h.projectService != nil && h.projectService.config != nil {
		maxFileSizeMB = h.projectService.config.ProjectWorkspaceMaxFileSizeMB
	}
	uploads, err := handlerutil.ReadWorkspaceUploads(input.RawBody, workspace.MaxFileSizeBytes(maxFileSizeMB))
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
		EnvironmentID: input.EnvironmentID, Type: activitytypes.TypeResourceAction, ResourceType: "project", ResourceID: input.ProjectID, ResourceName: h.projectActivityNameInternal(
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
		return nil, projectWorkspaceHTTPErrorInternal(err)
	}
	result.ActivityID = mo.EmptyableToOption(strings.TrimSpace(activityID)).ToPointer()
	return &handlerutil.Out[workspacetypes.Workspace]{Body: base.ApiResponse[workspacetypes.Workspace]{Success: true, Data: *result}}, nil
}
