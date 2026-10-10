package template

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/template"
	"go.getarcane.app/kit/pkg/mapping"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// TemplateHandler handles template management endpoints.
type TemplateHandler struct {
	templateService *TemplateService
}

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
	Body template.CreateRequest
}

type UpdateTemplateInput struct {
	ID   string `path:"id" doc:"Template ID"`
	Body template.UpdateRequest
}

type DeleteTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type DownloadTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type GetDefaultTemplatesInput struct{}

type SaveDefaultTemplatesInput struct {
	Body template.SaveDefaultTemplatesRequest
}

type GetTemplateRegistriesInput struct{}

type CreateTemplateRegistryInput struct {
	Body template.CreateRegistryRequest
}

type UpdateTemplateRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body template.UpdateRegistryRequest
}

type DeleteTemplateRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type FetchTemplateRegistryInput struct {
	URL string `query:"url" required:"true" doc:"Registry URL"`
}

// ListTemplates returns a paginated list of templates.
func (h *TemplateHandler) ListTemplates(ctx context.Context, input *ListTemplatesInput) (*handlerutil.Page[template.Template], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	params.Limit = cmp.Or(params.Limit, 20)
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	templates, paginationResp, err := h.templateService.GetAllTemplatesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get templates: " + err.Error())
	}

	return &handlerutil.Page[template.Template]{
		Body: base.Paginated[template.Template]{
			Success:    true,
			Data:       templates,
			Pagination: handlerutil.PaginationResponse(paginationResp),
		},
	}, nil
}

// GetAllTemplates returns all templates without pagination.
func (h *TemplateHandler) GetAllTemplates(ctx context.Context, _ *GetAllTemplatesInput) (*handlerutil.Out[[]template.Template], error) {
	templates, err := h.templateService.GetAllTemplates(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get templates: " + err.Error())
	}

	out, mapErr := mapping.MapSlice[ComposeTemplate, template.Template](templates)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map templates: " + mapErr.Error())
	}

	return &handlerutil.Out[[]template.Template]{
		Body: base.ApiResponse[[]template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplate returns a template by ID.
func (h *TemplateHandler) GetTemplate(ctx context.Context, input *GetTemplateInput) (*handlerutil.Out[template.Template], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	tmpl, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError("Failed to get template: " + err.Error())
	}

	var out template.Template
	if mapErr := mapping.MapStruct(tmpl, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map templates: " + mapErr.Error())
	}

	return &handlerutil.Out[template.Template]{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplateContent returns template content with parsed data.
func (h *TemplateHandler) GetTemplateContent(ctx context.Context, input *GetTemplateContentInput) (*handlerutil.Out[template.TemplateContent], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	contentData, err := h.templateService.GetTemplateContentWithParsedData(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError("Failed to get template content: " + err.Error())
	}

	return &handlerutil.Out[template.TemplateContent]{
		Body: base.ApiResponse[template.TemplateContent]{
			Success: true,
			Data:    *contentData,
		},
	}, nil
}

// CreateTemplate creates a new templatetypes.
func (h *TemplateHandler) CreateTemplate(ctx context.Context, input *CreateTemplateInput) (*handlerutil.Out[template.Template], error) {
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
		return nil, huma.Error500InternalServerError("Failed to create template: " + err.Error())
	}

	var out template.Template
	if mapErr := mapping.MapStruct(tmpl, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map templates: " + mapErr.Error())
	}

	return &handlerutil.Out[template.Template]{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateTemplate updates a templatetypes.
func (h *TemplateHandler) UpdateTemplate(ctx context.Context, input *UpdateTemplateInput) (*handlerutil.Out[template.Template], error) {
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
		return nil, huma.Error500InternalServerError("Failed to update template: " + err.Error())
	}

	updated, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to get template: " + err.Error())
	}

	var out template.Template
	if mapErr := mapping.MapStruct(updated, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map templates: " + mapErr.Error())
	}

	return &handlerutil.Out[template.Template]{
		Body: base.ApiResponse[template.Template]{
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
		return nil, huma.Error500InternalServerError("Failed to delete template: " + err.Error())
	}

	return handlerutil.MessageOutput("Template deleted successfully", ""), nil
}

// DownloadTemplate downloads a remote template to local storage.
func (h *TemplateHandler) DownloadTemplate(ctx context.Context, input *DownloadTemplateInput) (*handlerutil.Out[template.Template], error) {
	if input.ID == "" {
		return nil, huma.Error400BadRequest("Template ID is required")
	}

	tmpl, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		if errors.Is(err, common.ErrTemplateNotFound) {
			return nil, huma.Error404NotFound("Template not found")
		}
		return nil, huma.Error500InternalServerError("Failed to download template: " + err.Error())
	}
	if !tmpl.IsRemote {
		return nil, huma.Error400BadRequest("Template is already local")
	}

	localTemplate, err := h.templateService.DownloadTemplate(ctx, tmpl)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to download template: " + err.Error())
	}

	var out template.Template
	if mapErr := mapping.MapStruct(localTemplate, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map templates: " + mapErr.Error())
	}

	return &handlerutil.Out[template.Template]{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetDefaultTemplates returns the default compose and env templates.
func (h *TemplateHandler) GetDefaultTemplates(ctx context.Context, _ *GetDefaultTemplatesInput) (*handlerutil.Out[template.DefaultTemplatesResponse], error) {
	composeTemplate := h.templateService.GetComposeTemplate(ctx)
	swarmStackTemplate := h.templateService.GetSwarmStackTemplate(ctx)
	swarmStackEnvTemplate := h.templateService.GetSwarmStackEnvTemplate(ctx)
	envTemplate := h.templateService.GetEnvTemplate(ctx)

	return &handlerutil.Out[template.DefaultTemplatesResponse]{
		Body: base.ApiResponse[template.DefaultTemplatesResponse]{
			Success: true,
			Data: template.DefaultTemplatesResponse{
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
		return nil, huma.Error500InternalServerError("Failed to save default template: " + err.Error())
	}

	if err := h.templateService.SaveEnvTemplate(ctx, input.Body.EnvContent); err != nil {
		return nil, huma.Error500InternalServerError("Failed to save default template: " + err.Error())
	}

	return handlerutil.MessageOutput("Default templates saved successfully", ""), nil
}

// GetRegistries returns all template registries.
func (h *TemplateHandler) GetRegistries(ctx context.Context, _ *GetTemplateRegistriesInput) (*handlerutil.Out[[]template.TemplateRegistry], error) {
	registries, err := h.templateService.GetRegistries(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to fetch registry")
	}

	out, mapErr := mapping.MapSlice[TemplateRegistry, template.TemplateRegistry](registries)
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

	return &handlerutil.Out[[]template.TemplateRegistry]{
		Body: base.ApiResponse[[]template.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// CreateRegistry creates a new template registry.
func (h *TemplateHandler) CreateRegistry(ctx context.Context, input *CreateTemplateRegistryInput) (*handlerutil.Out[template.TemplateRegistry], error) {
	registry := &TemplateRegistry{
		Name:        input.Body.Name,
		URL:         input.Body.URL,
		Description: input.Body.Description,
		Enabled:     input.Body.Enabled,
	}
	if err := h.templateService.CreateRegistry(ctx, registry); err != nil {
		return nil, huma.Error500InternalServerError("Failed to create registry: " + err.Error())
	}

	var out template.TemplateRegistry
	if mapErr := mapping.MapStruct(registry, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError("Failed to map registry: " + mapErr.Error())
	}

	return &handlerutil.Out[template.TemplateRegistry]{
		Body: base.ApiResponse[template.TemplateRegistry]{
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
		return nil, huma.Error500InternalServerError("Failed to update registry: " + err.Error())
	}

	return handlerutil.MessageOutput("Registry updated successfully", ""), nil
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
		return nil, huma.Error500InternalServerError("Failed to delete registry: " + err.Error())
	}

	return handlerutil.MessageOutput("Registry deleted successfully", ""), nil
}

// FetchRegistry fetches templates from a remote registry URL.
func (h *TemplateHandler) FetchRegistry(ctx context.Context, input *FetchTemplateRegistryInput) (*handlerutil.Out[template.RemoteRegistry], error) {
	if input.URL == "" {
		return nil, huma.Error400BadRequest("Query parameter is required")
	}

	body, err := h.templateService.FetchRaw(ctx, input.URL)
	if err != nil {
		return nil, huma.Error502BadGateway("Failed to fetch registry")
	}

	var registry template.RemoteRegistry
	if unmarshalErr := json.Unmarshal(body, &registry); unmarshalErr != nil {
		return nil, huma.Error502BadGateway("Invalid JSON response")
	}

	return &handlerutil.Out[template.RemoteRegistry]{
		Body: base.ApiResponse[template.RemoteRegistry]{
			Success: true,
			Data:    registry,
		},
	}, nil
}
