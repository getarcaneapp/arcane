package handlers

import (
	"context"
	json "encoding/json/v2"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/template"
)

// TemplateHandler handles template management endpoints.
type TemplateHandler struct {
	templateService *services.TemplateService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// TemplatePaginatedResponse is the paginated response for templates.
type TemplatePaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []template.Template     `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListTemplatesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	Type   string `query:"type" doc:"Filter by template type (comma-separated: false,true)"`
}

type ListTemplatesOutput struct {
	Body TemplatePaginatedResponse
}

type GetAllTemplatesInput struct{}

type GetAllTemplatesOutput struct {
	Body base.ApiResponse[[]template.Template]
}

type GetTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type GetTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type GetTemplateContentInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type GetTemplateContentOutput struct {
	Body base.ApiResponse[template.TemplateContent]
}

type CreateTemplateInput struct {
	Body template.CreateRequest
}

type CreateTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type UpdateTemplateInput struct {
	ID   string `path:"id" doc:"Template ID"`
	Body template.UpdateRequest
}

type UpdateTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type DeleteTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type DeleteTemplateOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DownloadTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type DownloadTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type GetDefaultTemplatesInput struct{}

type GetDefaultTemplatesOutput struct {
	Body base.ApiResponse[template.DefaultTemplatesResponse]
}

type SaveDefaultTemplatesInput struct {
	Body template.SaveDefaultTemplatesRequest
}

type SaveDefaultTemplatesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type GetTemplateRegistriesInput struct{}

type GetTemplateRegistriesOutput struct {
	Body base.ApiResponse[[]template.TemplateRegistry]
}

type CreateTemplateRegistryInput struct {
	Body template.CreateRegistryRequest
}

type CreateTemplateRegistryOutput struct {
	Body base.ApiResponse[template.TemplateRegistry]
}

type UpdateTemplateRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body template.UpdateRegistryRequest
}

type UpdateTemplateRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DeleteTemplateRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type DeleteTemplateRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type FetchTemplateRegistryInput struct {
	URL string `query:"url" required:"true" doc:"Registry URL"`
}

type FetchTemplateRegistryOutput struct {
	Body base.ApiResponse[template.RemoteRegistry]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterTemplates registers all template management endpoints.
func RegisterTemplates(api huma.API, templateService *services.TemplateService) {
	h := &TemplateHandler{templateService: templateService}

	// Template registry endpoint.
	huma.Register(api, huma.Operation{
		OperationID: "fetchTemplateRegistry",
		Method:      "GET",
		Path:        "/templates/fetch",
		Summary:     "Fetch remote registry",
		Description: "Fetch templates from a remote registry URL",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.FetchRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "listTemplatesPaginated",
		Method:      "GET",
		Path:        "/templates",
		Summary:     "List templates (paginated)",
		Description: "Get a paginated list of compose templates",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesList),
	}, h.ListTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getAllTemplates",
		Method:      "GET",
		Path:        "/templates/all",
		Summary:     "List all templates",
		Description: "Get all compose templates without pagination",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesList),
	}, h.GetAllTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplate",
		Method:      "GET",
		Path:        "/templates/{id}",
		Summary:     "Get a template",
		Description: "Get a compose template by ID",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.GetTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplateContent",
		Method:      "GET",
		Path:        "/templates/{id}/content",
		Summary:     "Get template content",
		Description: "Get the compose content for a template with parsed data",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.GetTemplateContent)

	// Protected endpoints
	huma.Register(api, huma.Operation{
		OperationID: "createTemplate",
		Method:      "POST",
		Path:        "/templates",
		Summary:     "Create a template",
		Description: "Create a new compose template",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesCreate),
	}, h.CreateTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "updateTemplate",
		Method:      "PUT",
		Path:        "/templates/{id}",
		Summary:     "Update a template",
		Description: "Update an existing compose template",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesUpdate),
	}, h.UpdateTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "deleteTemplate",
		Method:      "DELETE",
		Path:        "/templates/{id}",
		Summary:     "Delete a template",
		Description: "Delete a compose template",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesDelete),
	}, h.DeleteTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "downloadTemplate",
		Method:      "POST",
		Path:        "/templates/{id}/download",
		Summary:     "Download a template",
		Description: "Download a remote template to local storage",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.DownloadTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "getDefaultTemplates",
		Method:      "GET",
		Path:        "/templates/default",
		Summary:     "Get default templates",
		Description: "Get the default compose and env templates",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.GetDefaultTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "saveDefaultTemplates",
		Method:      "POST",
		Path:        "/templates/default",
		Summary:     "Save default templates",
		Description: "Save the default compose and env templates",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesUpdate),
	}, h.SaveDefaultTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplateRegistries",
		Method:      "GET",
		Path:        "/templates/registries",
		Summary:     "List template registries",
		Description: "Get all template registries",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesList),
	}, h.GetRegistries)

	huma.Register(api, huma.Operation{
		OperationID: "createTemplateRegistry",
		Method:      "POST",
		Path:        "/templates/registries",
		Summary:     "Create a template registry",
		Description: "Create a new template registry",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesCreate),
	}, h.CreateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "updateTemplateRegistry",
		Method:      "PUT",
		Path:        "/templates/registries/{id}",
		Summary:     "Update a template registry",
		Description: "Update an existing template registry",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesUpdate),
	}, h.UpdateRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "deleteTemplateRegistry",
		Method:      "DELETE",
		Path:        "/templates/registries/{id}",
		Summary:     "Delete a template registry",
		Description: "Delete a template registry",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesDelete),
	}, h.DeleteRegistry)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListTemplates returns a paginated list of templates.
func (h *TemplateHandler) ListTemplates(ctx context.Context, input *ListTemplatesInput) (*ListTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParamsInternal(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	if params.Limit == 0 {
		params.Limit = 20
	}
	if input.Type != "" {
		params.Filters["type"] = input.Type
	}

	templates, paginationResp, err := h.templateService.GetAllTemplatesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateListError{Err: err}).Error())
	}

	return &ListTemplatesOutput{
		Body: TemplatePaginatedResponse{
			Success:    true,
			Data:       templates,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetAllTemplates returns all templates without pagination.
func (h *TemplateHandler) GetAllTemplates(ctx context.Context, _ *GetAllTemplatesInput) (*GetAllTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	templates, err := h.templateService.GetAllTemplates(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateListError{Err: err}).Error())
	}

	out, mapErr := mapper.MapSlice[models.ComposeTemplate, template.Template](templates)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateMappingError{Err: mapErr}).Error())
	}

	return &GetAllTemplatesOutput{
		Body: base.ApiResponse[[]template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplate returns a template by ID.
func (h *TemplateHandler) GetTemplate(ctx context.Context, input *GetTemplateInput) (*GetTemplateOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	// Path parameter arrives URL-encoded (e.g. "remote%3Areg%3Aslug" for remote IDs that
	// contain ':' separators). Chi/Huma do not auto-decode, so decode here before
	// matching against cached / stored template IDs.
	id, decodeErr := url.PathUnescape(input.ID)
	if decodeErr != nil {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	tmpl, err := h.templateService.GetTemplate(ctx, id)
	if err != nil {
		if common.IsTemplateNotFoundError(err) {
			return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.TemplateRetrievalError{Err: err}).Error())
	}

	var out template.Template
	if mapErr := mapper.MapStruct(tmpl, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateMappingError{Err: mapErr}).Error())
	}

	return &GetTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplateContent returns template content with parsed data.
func (h *TemplateHandler) GetTemplateContent(ctx context.Context, input *GetTemplateContentInput) (*GetTemplateContentOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	id, decodeErr := url.PathUnescape(input.ID)
	if decodeErr != nil {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	contentData, err := h.templateService.GetTemplateContentWithParsedData(ctx, id)
	if err != nil {
		if common.IsTemplateNotFoundError(err) {
			return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.TemplateContentError{Err: err}).Error())
	}

	return &GetTemplateContentOutput{
		Body: base.ApiResponse[template.TemplateContent]{
			Success: true,
			Data:    *contentData,
		},
	}, nil
}

// CreateTemplate creates a new template.
func (h *TemplateHandler) CreateTemplate(ctx context.Context, input *CreateTemplateInput) (*CreateTemplateOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	tmpl := &models.ComposeTemplate{
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
		return nil, huma.Error500InternalServerError((&common.TemplateCreationError{Err: err}).Error())
	}

	var out template.Template
	if mapErr := mapper.MapStruct(tmpl, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateMappingError{Err: mapErr}).Error())
	}

	return &CreateTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateTemplate updates a template.
func (h *TemplateHandler) UpdateTemplate(ctx context.Context, input *UpdateTemplateInput) (*UpdateTemplateOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	id, decodeErr := url.PathUnescape(input.ID)
	if decodeErr != nil {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	updates := &models.ComposeTemplate{
		Name:        input.Body.Name,
		Description: input.Body.Description,
		Content:     input.Body.Content,
	}
	if input.Body.EnvContent != "" {
		updates.EnvContent = &input.Body.EnvContent
	} else {
		updates.EnvContent = nil
	}

	if err := h.templateService.UpdateTemplate(ctx, id, updates); err != nil {
		if common.IsTemplateNotFoundError(err) {
			return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.TemplateUpdateError{Err: err}).Error())
	}

	updated, err := h.templateService.GetTemplate(ctx, id)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateRetrievalError{Err: err}).Error())
	}

	var out template.Template
	if mapErr := mapper.MapStruct(updated, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateMappingError{Err: mapErr}).Error())
	}

	return &UpdateTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteTemplate deletes a template.
func (h *TemplateHandler) DeleteTemplate(ctx context.Context, input *DeleteTemplateInput) (*DeleteTemplateOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	id, decodeErr := url.PathUnescape(input.ID)
	if decodeErr != nil {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	if err := h.templateService.DeleteTemplate(ctx, id); err != nil {
		if common.IsTemplateNotFoundError(err) {
			return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.TemplateDeletionError{Err: err}).Error())
	}

	return &DeleteTemplateOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Template deleted successfully",
			},
		},
	}, nil
}

// DownloadTemplate downloads a remote template to local storage.
func (h *TemplateHandler) DownloadTemplate(ctx context.Context, input *DownloadTemplateInput) (*DownloadTemplateOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	id, decodeErr := url.PathUnescape(input.ID)
	if decodeErr != nil {
		return nil, huma.Error400BadRequest((&common.TemplateIDRequiredError{}).Error())
	}

	tmpl, err := h.templateService.GetTemplate(ctx, id)
	if err != nil {
		if common.IsTemplateNotFoundError(err) {
			return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.TemplateDownloadError{Err: err}).Error())
	}
	if !tmpl.IsRemote {
		return nil, huma.Error400BadRequest((&common.TemplateAlreadyLocalError{}).Error())
	}

	localTemplate, err := h.templateService.DownloadTemplate(ctx, tmpl)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateDownloadError{Err: err}).Error())
	}

	var out template.Template
	if mapErr := mapper.MapStruct(localTemplate, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateMappingError{Err: mapErr}).Error())
	}

	return &DownloadTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetDefaultTemplates returns the default compose and env templates.
func (h *TemplateHandler) GetDefaultTemplates(ctx context.Context, _ *GetDefaultTemplatesInput) (*GetDefaultTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	composeTemplate := h.templateService.GetComposeTemplate()
	swarmStackTemplate := h.templateService.GetSwarmStackTemplate()
	swarmStackEnvTemplate := h.templateService.GetSwarmStackEnvTemplate()
	envTemplate := h.templateService.GetEnvTemplate()

	return &GetDefaultTemplatesOutput{
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
func (h *TemplateHandler) SaveDefaultTemplates(ctx context.Context, input *SaveDefaultTemplatesInput) (*SaveDefaultTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.templateService.SaveComposeTemplate(input.Body.ComposeContent); err != nil {
		return nil, huma.Error500InternalServerError((&common.DefaultTemplateSaveError{Err: err}).Error())
	}

	if err := h.templateService.SaveEnvTemplate(input.Body.EnvContent); err != nil {
		return nil, huma.Error500InternalServerError((&common.DefaultTemplateSaveError{Err: err}).Error())
	}

	return &SaveDefaultTemplatesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Default templates saved successfully",
			},
		},
	}, nil
}

// GetRegistries returns all template registries.
func (h *TemplateHandler) GetRegistries(ctx context.Context, _ *GetTemplateRegistriesInput) (*GetTemplateRegistriesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	registries, err := h.templateService.GetRegistries(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryFetchError{Err: err}).Error())
	}

	out, mapErr := mapper.MapSlice[models.TemplateRegistry, template.TemplateRegistry](registries)
	if mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryFetchError{Err: mapErr}).Error())
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

	return &GetTemplateRegistriesOutput{
		Body: base.ApiResponse[[]template.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// CreateRegistry creates a new template registry.
func (h *TemplateHandler) CreateRegistry(ctx context.Context, input *CreateTemplateRegistryInput) (*CreateTemplateRegistryOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	registry := &models.TemplateRegistry{
		Name:        input.Body.Name,
		URL:         input.Body.URL,
		Description: input.Body.Description,
		Enabled:     input.Body.Enabled,
	}
	if err := h.templateService.CreateRegistry(ctx, registry); err != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryCreationError{Err: err}).Error())
	}

	var out template.TemplateRegistry
	if mapErr := mapper.MapStruct(registry, &out); mapErr != nil {
		return nil, huma.Error500InternalServerError((&common.RegistryMappingError{Err: mapErr}).Error())
	}

	return &CreateTemplateRegistryOutput{
		Body: base.ApiResponse[template.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRegistry updates a template registry.
func (h *TemplateHandler) UpdateRegistry(ctx context.Context, input *UpdateTemplateRegistryInput) (*UpdateTemplateRegistryOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.RegistryIDRequiredError{}).Error())
	}

	updates := &models.TemplateRegistry{
		Name:        input.Body.Name,
		URL:         input.Body.URL,
		Description: input.Body.Description,
		Enabled:     input.Body.Enabled,
	}
	if err := h.templateService.UpdateRegistry(ctx, input.ID, updates); err != nil {
		if err.Error() == "registry not found" {
			return nil, huma.Error404NotFound((&common.RegistryNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.RegistryUpdateError{Err: err}).Error())
	}

	return &UpdateTemplateRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registry updated successfully",
			},
		},
	}, nil
}

// DeleteRegistry deletes a template registry.
func (h *TemplateHandler) DeleteRegistry(ctx context.Context, input *DeleteTemplateRegistryInput) (*DeleteTemplateRegistryOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.ID == "" {
		return nil, huma.Error400BadRequest((&common.RegistryIDRequiredError{}).Error())
	}

	if err := h.templateService.DeleteRegistry(ctx, input.ID); err != nil {
		if err.Error() == "registry not found" {
			return nil, huma.Error404NotFound((&common.RegistryNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.RegistryDeletionError{Err: err}).Error())
	}

	return &DeleteTemplateRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registry deleted successfully",
			},
		},
	}, nil
}

// FetchRegistry fetches templates from a remote registry URL.
func (h *TemplateHandler) FetchRegistry(ctx context.Context, input *FetchTemplateRegistryInput) (*FetchTemplateRegistryOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.URL == "" {
		return nil, huma.Error400BadRequest((&common.QueryParameterRequiredError{}).Error())
	}

	body, err := h.templateService.FetchRaw(ctx, input.URL)
	if err != nil {
		return nil, huma.Error502BadGateway((&common.RegistryFetchError{Err: err}).Error())
	}

	var registry template.RemoteRegistry
	if err := json.Unmarshal(body, &registry); err != nil {
		return nil, huma.Error502BadGateway((&common.InvalidJSONResponseError{Err: err}).Error())
	}

	return &FetchTemplateRegistryOutput{
		Body: base.ApiResponse[template.RemoteRegistry]{
			Success: true,
			Data:    registry,
		},
	}, nil
}
