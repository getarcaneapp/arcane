package secretsource

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/common"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	// SecretSourceHandler translates HTTP input for secret sources and project
	// secret bindings into service calls.
	SecretSourceHandler struct {
		service        *SecretSourceService
		resolveProject ProjectResolver
	}

	ListSourcesInput struct{}

	GetSourceInput struct {
		ID string `path:"id" doc:"Secret source ID"`
	}

	CreateSourceInput struct {
		Body types.SecretSourceCreateRequest
	}

	UpdateSourceInput struct {
		Body types.SecretSourceUpdateRequest
		ID   string `path:"id" doc:"Secret source ID"`
	}

	DeleteSourceInput struct {
		ID string `path:"id" doc:"Secret source ID"`
	}

	TestSourceInput struct {
		Body types.SecretSourceTestRequest
	}

	BrowseSourceInput struct {
		ID          string `path:"id" doc:"Secret source ID"`
		Kind        string `query:"kind" doc:"What to list. Infisical: projects, folders"`
		ProjectID   string `query:"projectId" doc:"Infisical project ID, for folders"`
		Environment string `query:"environment" doc:"Infisical environment slug, for folders"`
		Path        string `query:"path" doc:"Infisical folder path to list" default:"/"`
	}

	ProjectBindingInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
	}

	CreateProjectBindingInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
		Body          types.SecretSourceBindingUpsertRequest
	}

	UpdateProjectBindingInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
		BindingID     string `path:"bindingId" doc:"Binding ID"`
		Body          types.SecretSourceBindingUpsertRequest
	}

	ProjectBindingIDInput struct {
		EnvironmentID string `path:"id" doc:"Environment ID"`
		ProjectID     string `path:"projectId" doc:"Project ID"`
		BindingID     string `path:"bindingId" doc:"Binding ID"`
	}
)

func (h *SecretSourceHandler) ListSources(ctx context.Context, _ *ListSourcesInput) (*handlerutil.Out[[]types.SecretSource], error) {
	sources, err := h.service.ListSources(ctx)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(sources), nil
}

func (h *SecretSourceHandler) GetSource(ctx context.Context, input *GetSourceInput) (*handlerutil.Out[*types.SecretSource], error) {
	source, err := h.service.GetSource(ctx, input.ID)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(source), nil
}

func (h *SecretSourceHandler) CreateSource(ctx context.Context, input *CreateSourceInput) (*handlerutil.Out[*types.SecretSource], error) {
	source, err := h.service.CreateSource(ctx, input.Body)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(source), nil
}

func (h *SecretSourceHandler) UpdateSource(ctx context.Context, input *UpdateSourceInput) (*handlerutil.Out[*types.SecretSource], error) {
	source, err := h.service.UpdateSource(ctx, input.ID, input.Body)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(source), nil
}

func (h *SecretSourceHandler) DeleteSource(ctx context.Context, input *DeleteSourceInput) (*handlerutil.Out[types.MessageResponse], error) {
	if err := h.service.DeleteSource(ctx, input.ID); err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(types.MessageResponse{Message: "Secret source deleted"}), nil
}

func (h *SecretSourceHandler) TestSource(ctx context.Context, input *TestSourceInput) (*handlerutil.Out[types.SecretSourceTestResult], error) {
	result, err := h.service.TestSource(ctx, input.Body)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(result), nil
}

func (h *SecretSourceHandler) Browse(ctx context.Context, input *BrowseSourceInput) (*handlerutil.Out[[]types.SecretSourceBrowseItem], error) {
	items, err := h.service.Browse(ctx, input.ID, types.SecretSourceBrowseQuery{
		Kind:        input.Kind,
		ProjectID:   input.ProjectID,
		Environment: input.Environment,
		Path:        input.Path,
	})
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(items), nil
}

func (h *SecretSourceHandler) ListProjectBindings(ctx context.Context, input *ProjectBindingInput) (*handlerutil.Out[[]types.SecretSourceBinding], error) {
	if _, err := h.localProjectInternal(ctx, input.EnvironmentID, input.ProjectID); err != nil {
		return nil, err
	}
	bindings, err := h.service.ListBindings(ctx, input.ProjectID)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(bindings), nil
}

func (h *SecretSourceHandler) CreateProjectBinding(ctx context.Context, input *CreateProjectBindingInput) (*handlerutil.Out[*types.SecretSourceBinding], error) {
	if _, err := h.localProjectInternal(ctx, input.EnvironmentID, input.ProjectID); err != nil {
		return nil, err
	}
	binding, err := h.service.CreateBinding(ctx, input.ProjectID, input.Body)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(binding), nil
}

func (h *SecretSourceHandler) UpdateProjectBinding(ctx context.Context, input *UpdateProjectBindingInput) (*handlerutil.Out[*types.SecretSourceBinding], error) {
	if _, err := h.localProjectInternal(ctx, input.EnvironmentID, input.ProjectID); err != nil {
		return nil, err
	}
	binding, err := h.service.UpdateBinding(ctx, input.ProjectID, input.BindingID, input.Body)
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(binding), nil
}

func (h *SecretSourceHandler) DeleteProjectBinding(ctx context.Context, input *ProjectBindingIDInput) (*handlerutil.Out[types.MessageResponse], error) {
	if _, err := h.localProjectInternal(ctx, input.EnvironmentID, input.ProjectID); err != nil {
		return nil, err
	}
	if err := h.service.DeleteBinding(ctx, input.ProjectID, input.BindingID); err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(types.MessageResponse{Message: "Secret binding removed"}), nil
}

func (h *SecretSourceHandler) CheckProjectBindings(ctx context.Context, input *ProjectBindingInput) (*handlerutil.Out[types.SecretSourceProjectCheckResult], error) {
	project, err := h.localProjectInternal(ctx, input.EnvironmentID, input.ProjectID)
	if err != nil {
		return nil, err
	}
	result, err := h.service.CheckBindings(ctx, project, handlerutil.CurrentActor(ctx))
	if err != nil {
		return nil, httpErrorInternal(err)
	}
	return okInternal(result), nil
}

// localProjectInternal resolves a project of the local environment. Remote
// environments resolve bindings on their own agent, which this version does
// not support yet.
func (h *SecretSourceHandler) localProjectInternal(ctx context.Context, environmentID, projectID string) (ProjectRef, error) {
	if environmentID != localEnvironmentID {
		return ProjectRef{}, huma.Error400BadRequest("Secret sources are currently supported on the local environment only")
	}
	if h.resolveProject == nil {
		return ProjectRef{}, huma.Error500InternalServerError("project lookup is not configured")
	}
	project, err := h.resolveProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			return ProjectRef{}, huma.Error404NotFound(err.Error())
		}
		return ProjectRef{}, huma.Error500InternalServerError(err.Error())
	}
	return project, nil
}

func okInternal[T any](data T) *handlerutil.Out[T] {
	return &handlerutil.Out[T]{Body: types.ApiResponse[T]{Success: true, Data: data}}
}

func httpErrorInternal(err error) error {
	switch {
	case errors.Is(err, common.ErrValidation), errors.Is(err, common.ErrBadRequest):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, common.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, common.ErrConflict):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, common.ErrUnavailable):
		return huma.Error502BadGateway(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}
