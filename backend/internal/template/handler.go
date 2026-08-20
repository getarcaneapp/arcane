package template

import (
	"context"
	"encoding/json/v2"

	"emperror.dev/errors"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	templatetypes "github.com/getarcaneapp/arcane/types/v2/template"
)

// TemplateHandler handles template management endpoints.
type TemplateHandler struct {
	templateService *TemplateService
}

// ============================================================================
// Input/Output Types
// ============================================================================

type ListTemplatesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	Type   string `query:"type" doc:"Filter by template type (comma-separated: false,true)"`
}

type GetAllTemplatesInput struct{}

type GetTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type GetTemplateContentInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type CreateTemplateInput struct {
	Body templatetypes.CreateRequest
}

type UpdateTemplateInput struct {
	ID   string `path:"id" doc:"Template ID"`
	Body templatetypes.UpdateRequest
}

type DeleteTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type DownloadTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type GetDefaultTemplatesInput struct{}

type SaveDefaultTemplatesInput struct {
	Body templatetypes.SaveDefaultTemplatesRequest
}

type GetTemplateRegistriesInput struct{}

type CreateTemplateRegistryInput struct {
	Body templatetypes.CreateRegistryRequest
}

type UpdateTemplateRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body templatetypes.UpdateRegistryRequest
}

type DeleteTemplateRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type FetchTemplateRegistryInput struct {
	URL string `query:"url" required:"true" doc:"Registry URL"`
}

// ============================================================================
// Registration
// ============================================================================

// RegisterTemplates registers all template management endpoints.
func RegisterTemplates(api huma.API, templateService *TemplateService) {
	h := &TemplateHandler{templateService: templateService}

	// Template registry endpoint.
	huma.Register(api, huma.Operation{
		OperationID: "fetchTemplateRegistry",
		Method:      "GET",
		Path:        "/templates/fetch",
		Summary:     "Fetch remote registry",
		Description: "Fetch templates from a remote registry URL",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesRead),
	}, h.FetchRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "listTemplatesPaginated",
		Method:      "GET",
		Path:        "/templates",
		Summary:     "List templates (paginated)",
		Description: "Get a paginated list of compose templates",
		Tags:        []string{"Templates"},
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesList),
	}, h.ListTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getAllTemplates",
		Method:      "GET",
		Path:        "/templates/all",
		Summary:     "List all templates",
		Description: "Get all compose templates without pagination",
		Tags:        []string{"Templates"},
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesList),
	}, h.GetAllTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplate",
		Method:      "GET",
		Path:        "/templates/{id}",
		Summary:     "Get a template",
		Description: "Get a compose template by ID",
		Tags:        []string{"Templates"},
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesRead),
	}, h.GetTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplateContent",
		Method:      "GET",
		Path:        "/templates/{id}/content",
		Summary:     "Get template content",
		Description: "Get the compose content for a template with parsed data",
		Tags:        []string{"Templates"},
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesRead),
	}, h.GetTemplateContent)

	// Protected endpoints
	huma.Register(api, huma.Operation{
		OperationID: "createTemplate",
		Method:      "POST",
		Path:        "/templates",
		Summary:     "Create a template",
		Description: "Create a new compose template",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesCreate),
	}, h.CreateTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "updateTemplate",
		Method:      "PUT",
		Path:        "/templates/{id}",
		Summary:     "Update a template",
		Description: "Update an existing compose template",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesUpdate),
	}, h.UpdateTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "deleteTemplate",
		Method:      "DELETE",
		Path:        "/templates/{id}",
		Summary:     "Delete a template",
		Description: "Delete a compose template",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesDelete),
	}, h.DeleteTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "downloadTemplate",
		Method:      "POST",
		Path:        "/templates/{id}/download",
		Summary:     "Download a template",
		Description: "Download a remote template to local storage",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesRead),
	}, h.DownloadTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "getDefaultTemplates",
		Method:      "GET",
		Path:        "/templates/default",
		Summary:     "Get default templates",
		Description: "Get the default compose and env templates",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesRead),
	}, h.GetDefaultTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "saveDefaultTemplates",
		Method:      "POST",
		Path:        "/templates/default",
		Summary:     "Save default templates",
		Description: "Save the default compose and env templates",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesUpdate),
	}, h.SaveDefaultTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplateRegistries",
		Method:      "GET",
		Path:        "/templates/registries",
		Summary:     "List template registries",
		Description: "Get all template registries",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesList),
	}, h.GetRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "createTemplateRegistry",
		Method:      "POST",
		Path:        "/templates/registries",
		Summary:     "Create a template registry",
		Description: "Create a new template registry",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesCreate),
	}, h.CreateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "updateTemplateRegistry",
		Method:      "PUT",
		Path:        "/templates/registries/{id}",
		Summary:     "Update a template registry",
		Description: "Update an existing template registry",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesUpdate),
	}, h.UpdateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "deleteTemplateRegistry",
		Method:      "DELETE",
		Path:        "/templates/registries/{id}",
		Summary:     "Delete a template registry",
		Description: "Delete a template registry",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermTemplatesDelete),
	}, h.DeleteRegistry)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListTemplates returns a paginated list of templates.
func (h *TemplateHandler) ListTemplates(ctx context.Context, input *ListTemplatesInput) (*handlerutil.Page[templatetypes.Template], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if params.Limit == 0 {
		params.Limit = 20
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	templates, paginationResp, err := h.templateService.GetAllTemplatesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get templates").Error())
	}

	return &handlerutil.Page[templatetypes.Template]{
		Body: base.Paginated[templatetypes.Template]{
			Success:    true,
			Data:       templates,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetAllTemplates returns all templates without pagination.
func (h *TemplateHandler) GetAllTemplates(ctx context.Context, _ *GetAllTemplatesInput) (*handlerutil.Out[[]templatetypes.Template], error) {
	templates, err := h.templateService.GetAllTemplates(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get templates").Error())
	}

	out, mapErr := mapper.MapSlice[ComposeTemplate, templatetypes.Template](templates)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(mapErr, "Failed to map templates").Error())
	}

	return &handlerutil.Out[[]templatetypes.Template]{
		Body: base.ApiResponse[[]templatetypes.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplate returns a template by ID.
func (h *TemplateHandler) GetTemplate(ctx context.Context, input *GetTemplateInput) (*handlerutil.Out[templatetypes.Template], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	tmpl, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get template").Error())
	}

	var out templatetypes.Template
	if mapErr := mapper.MapStruct(tmpl, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(mapErr, "Failed to map templates").Error())
	}

	return &handlerutil.Out[templatetypes.Template]{
		Body: base.ApiResponse[templatetypes.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplateContent returns template content with parsed data.
func (h *TemplateHandler) GetTemplateContent(ctx context.Context, input *GetTemplateContentInput) (*handlerutil.Out[templatetypes.TemplateContent], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	contentData, err := h.templateService.GetTemplateContentWithParsedData(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get template content").Error())
	}

	return &handlerutil.Out[templatetypes.TemplateContent]{
		Body: base.ApiResponse[templatetypes.TemplateContent]{
			Success: true,
			Data:    *contentData,
		},
	}, nil
}

// CreateTemplate creates a new templatetypes.
func (h *TemplateHandler) CreateTemplate(ctx context.Context, input *CreateTemplateInput) (*handlerutil.Out[templatetypes.Template], error) {
	tmpl := &ComposeTemplate{
		Name:        input.Body.Name,
		Description: input.Body.Description,
		Content:     input.Body.Content,
		IsCustom:    true,
		IsRemote:    false,
	}
	if input.Body.EnvContent != "" {
		tmpl.EnvContent = &input.Body.EnvContent
	}

	if err := h.templateService.CreateTemplate(ctx, tmpl); err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to create template").Error())
	}

	var out templatetypes.Template
	if mapErr := mapper.MapStruct(tmpl, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(mapErr, "Failed to map templates").Error())
	}

	return &handlerutil.Out[templatetypes.Template]{
		Body: base.ApiResponse[templatetypes.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateTemplate updates a templatetypes.
func (h *TemplateHandler) UpdateTemplate(ctx context.Context, input *UpdateTemplateInput) (*handlerutil.Out[templatetypes.Template], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	updates := &ComposeTemplate{
		Name:        input.Body.Name,
		Description: input.Body.Description,
		Content:     input.Body.Content,
	}
	if input.Body.EnvContent != "" {
		updates.EnvContent = &input.Body.EnvContent
	} else {
		updates.EnvContent = nil
	}

	if err := h.templateService.UpdateTemplate(ctx, input.ID, updates); err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to update template").Error())
	}

	updated, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to get template").Error())
	}

	var out templatetypes.Template
	if mapErr := mapper.MapStruct(updated, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(mapErr, "Failed to map templates").Error())
	}

	return &handlerutil.Out[templatetypes.Template]{
		Body: base.ApiResponse[templatetypes.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteTemplate deletes a templatetypes.
func (h *TemplateHandler) DeleteTemplate(ctx context.Context, input *DeleteTemplateInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	if err := h.templateService.DeleteTemplate(ctx, input.ID); err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to delete template").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Template deleted successfully",
			},
		},
	}, nil
}

// DownloadTemplate downloads a remote template to local storage.
func (h *TemplateHandler) DownloadTemplate(ctx context.Context, input *DownloadTemplateInput) (*handlerutil.Out[templatetypes.Template], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	tmpl, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to download template").Error())
	}
	if !tmpl.IsRemote {
		return nil, huma.Error400BadRequest("Template is already local")
	}

	localTemplate, err := h.templateService.DownloadTemplate(ctx, tmpl)
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to download template").Error())
	}

	var out templatetypes.Template
	if mapErr := mapper.MapStruct(localTemplate, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(mapErr, "Failed to map templates").Error())
	}

	return &handlerutil.Out[templatetypes.Template]{
		Body: base.ApiResponse[templatetypes.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetDefaultTemplates returns the default compose and env templates.
func (h *TemplateHandler) GetDefaultTemplates(ctx context.Context, _ *GetDefaultTemplatesInput) (*handlerutil.Out[templatetypes.DefaultTemplatesResponse], error) {
	composeTemplate := h.templateService.GetComposeTemplate(ctx)
	swarmStackTemplate := h.templateService.GetSwarmStackTemplate(ctx)
	swarmStackEnvTemplate := h.templateService.GetSwarmStackEnvTemplate(ctx)
	envTemplate := h.templateService.GetEnvTemplate(ctx)

	return &handlerutil.Out[templatetypes.DefaultTemplatesResponse]{
		Body: base.ApiResponse[templatetypes.DefaultTemplatesResponse]{
			Success: true,
			Data: templatetypes.DefaultTemplatesResponse{
				ComposeTemplate:       composeTemplate,
				SwarmStackTemplate:    swarmStackTemplate,
				SwarmStackEnvTemplate: swarmStackEnvTemplate,
				EnvTemplate:           envTemplate,
			},
		},
	}, nil
}

// SaveDefaultTemplates saves the default compose and env templates.
func (h *TemplateHandler) SaveDefaultTemplates(ctx context.Context, input *SaveDefaultTemplatesInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.templateService.SaveComposeTemplate(ctx, input.Body.ComposeContent); err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to save default template").Error())
	}

	if err := h.templateService.SaveEnvTemplate(ctx, input.Body.EnvContent); err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to save default template").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Default templates saved successfully",
			},
		},
	}, nil
}

// GetRegistries returns all template registries.
func (h *TemplateHandler) GetRegistries(ctx context.Context, _ *GetTemplateRegistriesInput) (*handlerutil.Out[[]templatetypes.TemplateRegistry], error) {
	registries, err := h.templateService.GetRegistries(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch registry")
	}

	out, mapErr := mapper.MapSlice[TemplateRegistry, templatetypes.TemplateRegistry](registries)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch registry")
	}

	// Overlay the last fetch error from the in-memory tracker so the UI can
	// display why a registry is not returning templates without requiring the
	// user to check server logs.
	fetchErrors := h.templateService.GetRegistryFetchErrors()
	for i := range out {
		if msg, ok := fetchErrors[out[i].ID]; ok {
			out[i].LastFetchError = &msg
		}
	}

	return &handlerutil.Out[[]templatetypes.TemplateRegistry]{
		Body: base.ApiResponse[[]templatetypes.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// CreateRegistry creates a new template registry.
func (h *TemplateHandler) CreateRegistry(ctx context.Context, input *CreateTemplateRegistryInput) (*handlerutil.Out[templatetypes.TemplateRegistry], error) {
	registry := &TemplateRegistry{
		Name:        input.Body.Name,
		URL:         input.Body.URL,
		Description: input.Body.Description,
		Enabled:     input.Body.Enabled,
	}
	if err := h.templateService.CreateRegistry(ctx, registry); err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to create registry").Error())
	}

	var out templatetypes.TemplateRegistry
	if mapErr := mapper.MapStruct(registry, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(mapErr, "Failed to map registry").Error())
	}

	return &handlerutil.Out[templatetypes.TemplateRegistry]{
		Body: base.ApiResponse[templatetypes.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRegistry updates a template registry.
func (h *TemplateHandler) UpdateRegistry(ctx context.Context, input *UpdateTemplateRegistryInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Registry ID is required")
	}

	updates := &TemplateRegistry{
		Name:        input.Body.Name,
		URL:         input.Body.URL,
		Description: input.Body.Description,
		Enabled:     input.Body.Enabled,
	}
	if err := h.templateService.UpdateRegistry(ctx, input.ID, updates); err != nil {
		if err.Error() == "registry not found" {
			return nil, huma.Error404NotFound("Registry not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to update registry").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registry updated successfully",
			},
		},
	}, nil
}

// DeleteRegistry deletes a template registry.
func (h *TemplateHandler) DeleteRegistry(ctx context.Context, input *DeleteTemplateRegistryInput) (*handlerutil.Out[base.MessageResponse], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Registry ID is required")
	}

	if err := h.templateService.DeleteRegistry(ctx, input.ID); err != nil {
		if err.Error() == "registry not found" {
			return nil, huma.Error404NotFound("Registry not found")
		}
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to delete registry").Error())
	}

	return &handlerutil.Out[base.MessageResponse]{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registry deleted successfully",
			},
		},
	}, nil
}

// FetchRegistry fetches templates from a remote registry URL.
func (h *TemplateHandler) FetchRegistry(ctx context.Context, input *FetchTemplateRegistryInput) (*handlerutil.Out[templatetypes.RemoteRegistry], error) {
	if input.URL == "" {
		return nil, huma.Error400BadRequest("Query parameter is required")
	}

	body, err := h.templateService.FetchRaw(ctx, input.URL)
	if err != nil {
		return nil, huma.Error502BadGateway("Failed to fetch registry")
	}

	var registry templatetypes.RemoteRegistry
	if err := json.Unmarshal(body, &registry); err != nil {
		return nil, huma.Error502BadGateway("Invalid JSON response")
	}

	return &handlerutil.Out[templatetypes.RemoteRegistry]{
		Body: base.ApiResponse[templatetypes.RemoteRegistry]{
			Success: true,
			Data:    registry,
		},
	}, nil
}
