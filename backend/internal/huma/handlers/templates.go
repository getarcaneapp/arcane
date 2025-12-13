package handlers

import (
	"context"
	"encoding/json"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/mapper"
	"github.com/getarcaneapp/arcane/types/base"
	"github.com/getarcaneapp/arcane/types/env"
	"github.com/getarcaneapp/arcane/types/template"
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
	Page    int    `query:"pagination[page]" default:"1" doc:"Page number"`
	Limit   int    `query:"pagination[limit]" default:"20" doc:"Items per page"`
	SortCol string `query:"sort[column]" doc:"Column to sort by"`
	SortDir string `query:"sort[direction]" default:"asc" doc:"Sort direction"`
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

type GetGlobalVariablesInput struct{}

type GetGlobalVariablesOutput struct {
	Body base.ApiResponse[[]env.Variable]
}

type UpdateGlobalVariablesInput struct {
	Body env.Summary
}

type UpdateGlobalVariablesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterTemplates registers all template management endpoints.
func RegisterTemplates(api huma.API, templateService *services.TemplateService) {
	h := &TemplateHandler{templateService: templateService}

	// Public endpoints (no auth required in original)
	huma.Register(api, huma.Operation{
		OperationID: "fetchTemplateRegistry",
		Method:      "GET",
		Path:        "/templates/fetch",
		Summary:     "Fetch remote registry",
		Description: "Fetch templates from a remote registry URL",
		Tags:        []string{"Templates"},
	}, h.FetchRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "listTemplatesPaginated",
		Method:      "GET",
		Path:        "/templates",
		Summary:     "List templates (paginated)",
		Description: "Get a paginated list of compose templates",
		Tags:        []string{"Templates"},
	}, h.ListTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getAllTemplates",
		Method:      "GET",
		Path:        "/templates/all",
		Summary:     "List all templates",
		Description: "Get all compose templates without pagination",
		Tags:        []string{"Templates"},
	}, h.GetAllTemplates)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplate",
		Method:      "GET",
		Path:        "/templates/{id}",
		Summary:     "Get a template",
		Description: "Get a compose template by ID",
		Tags:        []string{"Templates"},
	}, h.GetTemplate)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplateContent",
		Method:      "GET",
		Path:        "/templates/{id}/content",
		Summary:     "Get template content",
		Description: "Get the compose content for a template with parsed data",
		Tags:        []string{"Templates"},
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
	}, h.DeleteRegistry)

	huma.Register(api, huma.Operation{
		OperationID: "getGlobalVariables",
		Method:      "GET",
		Path:        "/templates/variables",
		Summary:     "Get global variables",
		Description: "Get global template variables",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.GetGlobalVariables)

	huma.Register(api, huma.Operation{
		OperationID: "updateGlobalVariables",
		Method:      "PUT",
		Path:        "/templates/variables",
		Summary:     "Update global variables",
		Description: "Update global template variables",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, h.UpdateGlobalVariables)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListTemplates returns a paginated list of templates.
func (h *TemplateHandler) ListTemplates(ctx context.Context, input *ListTemplatesInput) (*ListTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildPaginationParams(input.Page, input.Limit, input.SortCol, input.SortDir)
	if params.Limit == 0 {
		params.Limit = 20
	}

	templates, paginationResp, err := h.templateService.GetAllTemplatesPaginated(ctx, params)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.TemplateListError{Err: err}).Error())
	}

	return &ListTemplatesOutput{
		Body: TemplatePaginatedResponse{
			Success: true,
			Data:    templates,
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

	tmpl, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		if err.Error() == "template not found" {
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

	contentData, err := h.templateService.GetTemplateContentWithParsedData(ctx, input.ID)
	if err != nil {
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

	if err := h.templateService.UpdateTemplate(ctx, input.ID, updates); err != nil {
		if err.Error() == "template not found" {
			return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
		}
		return nil, huma.Error500InternalServerError((&common.TemplateUpdateError{Err: err}).Error())
	}

	updated, err := h.templateService.GetTemplate(ctx, input.ID)
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

	if err := h.templateService.DeleteTemplate(ctx, input.ID); err != nil {
		if err.Error() == "template not found" {
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

	tmpl, err := h.templateService.GetTemplate(ctx, input.ID)
	if err != nil {
		return nil, huma.Error404NotFound((&common.TemplateNotFoundError{}).Error())
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
	envTemplate := h.templateService.GetEnvTemplate()

	return &GetDefaultTemplatesOutput{
		Body: base.ApiResponse[template.DefaultTemplatesResponse]{
			Success: true,
			Data: template.DefaultTemplatesResponse{
				ComposeTemplate: composeTemplate,
				EnvTemplate:     envTemplate,
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

// GetGlobalVariables returns global template variables.
func (h *TemplateHandler) GetGlobalVariables(ctx context.Context, _ *GetGlobalVariablesInput) (*GetGlobalVariablesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	vars, err := h.templateService.GetGlobalVariables(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GlobalVariablesRetrievalError{Err: err}).Error())
	}

	return &GetGlobalVariablesOutput{
		Body: base.ApiResponse[[]env.Variable]{
			Success: true,
			Data:    vars,
		},
	}, nil
}

// UpdateGlobalVariables updates global template variables.
func (h *TemplateHandler) UpdateGlobalVariables(ctx context.Context, input *UpdateGlobalVariablesInput) (*UpdateGlobalVariablesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.templateService.UpdateGlobalVariables(ctx, input.Body.Variables); err != nil {
		return nil, huma.Error500InternalServerError((&common.GlobalVariablesUpdateError{Err: err}).Error())
	}

	return &UpdateGlobalVariablesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Global variables updated successfully",
			},
		},
	}, nil
}
