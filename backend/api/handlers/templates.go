package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/mapper"
	"github.com/getarcaneapp/arcane/types/v2/base"
	"github.com/getarcaneapp/arcane/types/v2/env"
	"github.com/getarcaneapp/arcane/types/v2/template"
)

// templateHandler handles template management endpoints.
type templateHandler struct {
	templateService    *services.TemplateService
	environmentService *services.EnvironmentService
}

// ============================================================================
// Input/Output Types
// ============================================================================

// templatePaginatedResponse is the paginated response for templates.
type templatePaginatedResponse struct {
	Success    bool                    `json:"success"`
	Data       []template.Template     `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type listTemplatesInput struct {
	Search string `query:"search" doc:"Search query"`
	Sort   string `query:"sort" doc:"Column to sort by"`
	Order  string `query:"order" default:"asc" doc:"Sort direction"`
	Start  int    `query:"start" default:"0" doc:"Start index"`
	Limit  int    `query:"limit" default:"20" doc:"Items per page"`
	Type   string `query:"type" doc:"Filter by template type (comma-separated: false,true)"`
}

type listTemplatesOutput struct {
	Body templatePaginatedResponse
}

type getAllTemplatesInput struct{}

type getAllTemplatesOutput struct {
	Body base.ApiResponse[[]template.Template]
}

type getTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type getTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type getTemplateContentInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type getTemplateContentOutput struct {
	Body base.ApiResponse[template.TemplateContent]
}

type createTemplateInput struct {
	Body template.CreateRequest
}

type createTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type updateTemplateInput struct {
	ID   string `path:"id" doc:"Template ID"`
	Body template.UpdateRequest
}

type updateTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type deleteTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type deleteTemplateOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type downloadTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type downloadTemplateOutput struct {
	Body base.ApiResponse[template.Template]
}

type getDefaultTemplatesInput struct{}

type getDefaultTemplatesOutput struct {
	Body base.ApiResponse[template.DefaultTemplatesResponse]
}

type saveDefaultTemplatesInput struct {
	Body template.SaveDefaultTemplatesRequest
}

type saveDefaultTemplatesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type getTemplateRegistriesInput struct{}

type getTemplateRegistriesOutput struct {
	Body base.ApiResponse[[]template.TemplateRegistry]
}

type createTemplateRegistryInput struct {
	Body template.CreateRegistryRequest
}

type createTemplateRegistryOutput struct {
	Body base.ApiResponse[template.TemplateRegistry]
}

type updateTemplateRegistryInput struct {
	ID   string `path:"id" doc:"Registry ID"`
	Body template.UpdateRegistryRequest
}

type updateTemplateRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type deleteTemplateRegistryInput struct {
	ID string `path:"id" doc:"Registry ID"`
}

type deleteTemplateRegistryOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type fetchTemplateRegistryInput struct {
	URL string `query:"url" required:"true" doc:"Registry URL"`
}

type fetchTemplateRegistryOutput struct {
	Body base.ApiResponse[template.RemoteRegistry]
}

type getGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type getGlobalVariablesOutput struct {
	Body base.ApiResponse[[]env.Variable]
}

type updateGlobalVariablesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          env.Summary
}

type updateGlobalVariablesOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

// ============================================================================
// Registration
// ============================================================================

// RegisterTemplates registers all template management endpoints.
func RegisterTemplates(api huma.API, templateService *services.TemplateService, environmentService *services.EnvironmentService) {
	h := &templateHandler{templateService: templateService, environmentService: environmentService}

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
	}, h.fetchRegistryInternal)

	huma.Register(api, huma.Operation{
		OperationID: "listTemplatesPaginated",
		Method:      "GET",
		Path:        "/templates",
		Summary:     "List templates (paginated)",
		Description: "Get a paginated list of compose templates",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesList),
	}, h.listTemplatesInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getAllTemplates",
		Method:      "GET",
		Path:        "/templates/all",
		Summary:     "List all templates",
		Description: "Get all compose templates without pagination",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesList),
	}, h.getAllTemplatesInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplate",
		Method:      "GET",
		Path:        "/templates/{id}",
		Summary:     "Get a template",
		Description: "Get a compose template by ID",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.getTemplateInternal)

	huma.Register(api, huma.Operation{
		OperationID: "getTemplateContent",
		Method:      "GET",
		Path:        "/templates/{id}/content",
		Summary:     "Get template content",
		Description: "Get the compose content for a template with parsed data",
		Tags:        []string{"Templates"},
		Middlewares: humamw.RequirePermission(api, authz.PermTemplatesRead),
	}, h.getTemplateContentInternal)

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
	}, h.createTemplateInternal)

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
	}, h.updateTemplateInternal)

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
	}, h.deleteTemplateInternal)

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
	}, h.downloadTemplateInternal)

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
	}, h.getDefaultTemplatesInternal)

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
	}, h.saveDefaultTemplatesInternal)

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
	}, h.getRegistriesInternal)

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
	}, h.createRegistryInternal)

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
	}, h.updateRegistryInternal)

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
	}, h.deleteRegistryInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "getGlobalVariables",
		Method:      "GET",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Get global variables",
		Description: "Get global template variables for an environment",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesRead, h.getGlobalVariablesInternal)

	humamw.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateGlobalVariables",
		Method:      "PUT",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Update global variables",
		Description: "Update global template variables for an environment",
		Tags:        []string{"Templates"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
	}, authz.PermTemplatesUpdate, h.updateGlobalVariablesInternal)
}

// ============================================================================
// Handler Methods
// ============================================================================

// ListTemplates returns a paginated list of templates.
func (h *templateHandler) listTemplatesInternal(ctx context.Context, input *listTemplatesInput) (*listTemplatesOutput, error) {
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

	return &listTemplatesOutput{
		Body: templatePaginatedResponse{
			Success:    true,
			Data:       templates,
			Pagination: toPaginationResponseInternal(paginationResp),
		},
	}, nil
}

// GetAllTemplates returns all templates without pagination.
func (h *templateHandler) getAllTemplatesInternal(ctx context.Context, _ *getAllTemplatesInput) (*getAllTemplatesOutput, error) {
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

	return &getAllTemplatesOutput{
		Body: base.ApiResponse[[]template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplate returns a template by ID.
func (h *templateHandler) getTemplateInternal(ctx context.Context, input *getTemplateInput) (*getTemplateOutput, error) {
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

	return &getTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetTemplateContent returns template content with parsed data.
func (h *templateHandler) getTemplateContentInternal(ctx context.Context, input *getTemplateContentInput) (*getTemplateContentOutput, error) {
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

	return &getTemplateContentOutput{
		Body: base.ApiResponse[template.TemplateContent]{
			Success: true,
			Data:    *contentData,
		},
	}, nil
}

// CreateTemplate creates a new template.
func (h *templateHandler) createTemplateInternal(ctx context.Context, input *createTemplateInput) (*createTemplateOutput, error) {
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

	return &createTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateTemplate updates a template.
func (h *templateHandler) updateTemplateInternal(ctx context.Context, input *updateTemplateInput) (*updateTemplateOutput, error) {
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

	return &updateTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// DeleteTemplate deletes a template.
func (h *templateHandler) deleteTemplateInternal(ctx context.Context, input *deleteTemplateInput) (*deleteTemplateOutput, error) {
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

	return &deleteTemplateOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Template deleted successfully",
			},
		},
	}, nil
}

// DownloadTemplate downloads a remote template to local storage.
func (h *templateHandler) downloadTemplateInternal(ctx context.Context, input *downloadTemplateInput) (*downloadTemplateOutput, error) {
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

	return &downloadTemplateOutput{
		Body: base.ApiResponse[template.Template]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// GetDefaultTemplates returns the default compose and env templates.
func (h *templateHandler) getDefaultTemplatesInternal(_ context.Context, _ *getDefaultTemplatesInput) (*getDefaultTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	composeTemplate := h.templateService.GetComposeTemplate()
	swarmStackTemplate := h.templateService.GetSwarmStackTemplate()
	swarmStackEnvTemplate := h.templateService.GetSwarmStackEnvTemplate()
	envTemplate := h.templateService.GetEnvTemplate()

	return &getDefaultTemplatesOutput{
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
func (h *templateHandler) saveDefaultTemplatesInternal(_ context.Context, input *saveDefaultTemplatesInput) (*saveDefaultTemplatesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if err := h.templateService.SaveComposeTemplate(input.Body.ComposeContent); err != nil {
		return nil, huma.Error500InternalServerError((&common.DefaultTemplateSaveError{Err: err}).Error())
	}

	if err := h.templateService.SaveEnvTemplate(input.Body.EnvContent); err != nil {
		return nil, huma.Error500InternalServerError((&common.DefaultTemplateSaveError{Err: err}).Error())
	}

	return &saveDefaultTemplatesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Default templates saved successfully",
			},
		},
	}, nil
}

// GetRegistries returns all template registries.
func (h *templateHandler) getRegistriesInternal(ctx context.Context, _ *getTemplateRegistriesInput) (*getTemplateRegistriesOutput, error) {
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

	return &getTemplateRegistriesOutput{
		Body: base.ApiResponse[[]template.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// CreateRegistry creates a new template registry.
func (h *templateHandler) createRegistryInternal(ctx context.Context, input *createTemplateRegistryInput) (*createTemplateRegistryOutput, error) {
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

	return &createTemplateRegistryOutput{
		Body: base.ApiResponse[template.TemplateRegistry]{
			Success: true,
			Data:    out,
		},
	}, nil
}

// UpdateRegistry updates a template registry.
func (h *templateHandler) updateRegistryInternal(ctx context.Context, input *updateTemplateRegistryInput) (*updateTemplateRegistryOutput, error) {
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

	return &updateTemplateRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registry updated successfully",
			},
		},
	}, nil
}

// DeleteRegistry deletes a template registry.
func (h *templateHandler) deleteRegistryInternal(ctx context.Context, input *deleteTemplateRegistryInput) (*deleteTemplateRegistryOutput, error) {
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

	return &deleteTemplateRegistryOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Registry deleted successfully",
			},
		},
	}, nil
}

// FetchRegistry fetches templates from a remote registry URL.
func (h *templateHandler) fetchRegistryInternal(ctx context.Context, input *fetchTemplateRegistryInput) (*fetchTemplateRegistryOutput, error) {
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

	return &fetchTemplateRegistryOutput{
		Body: base.ApiResponse[template.RemoteRegistry]{
			Success: true,
			Data:    registry,
		},
	}, nil
}

// GetGlobalVariables returns global template variables.
func (h *templateHandler) getGlobalVariablesInternal(ctx context.Context, input *getGlobalVariablesInput) (*getGlobalVariablesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID != "0" {
		return h.getGlobalVariablesForRemoteEnvironmentInternal(ctx, input)
	}

	vars, err := h.templateService.GetGlobalVariables(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError((&common.GlobalVariablesRetrievalError{Err: err}).Error())
	}

	return &getGlobalVariablesOutput{
		Body: base.ApiResponse[[]env.Variable]{
			Success: true,
			Data:    vars,
		},
	}, nil
}

func (h *templateHandler) getGlobalVariablesForRemoteEnvironmentInternal(ctx context.Context, input *getGlobalVariablesInput) (*getGlobalVariablesOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}

	response, err := proxyRemoteJSONInternal[base.ApiResponse[[]env.Variable]](ctx, h.environmentService, input.EnvironmentID, http.MethodGet, "/api/environments/0/templates/variables", nil)
	if err != nil {
		return nil, err
	}

	return &getGlobalVariablesOutput{Body: *response}, nil
}

// UpdateGlobalVariables updates global template variables.
func (h *templateHandler) updateGlobalVariablesInternal(ctx context.Context, input *updateGlobalVariablesInput) (*updateGlobalVariablesOutput, error) {
	if h.templateService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	if input.EnvironmentID != "0" {
		return h.updateGlobalVariablesForRemoteEnvironmentInternal(ctx, input)
	}

	if err := h.templateService.UpdateGlobalVariables(ctx, input.Body.Variables); err != nil {
		if common.IsInvalidEnvKeyError(err) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return nil, huma.Error500InternalServerError((&common.GlobalVariablesUpdateError{Err: err}).Error())
	}

	return &updateGlobalVariablesOutput{
		Body: base.ApiResponse[base.MessageResponse]{
			Success: true,
			Data: base.MessageResponse{
				Message: "Global variables updated successfully",
			},
		},
	}, nil
}

func (h *templateHandler) updateGlobalVariablesForRemoteEnvironmentInternal(ctx context.Context, input *updateGlobalVariablesInput) (*updateGlobalVariablesOutput, error) {
	if h.environmentService == nil {
		return nil, huma.Error500InternalServerError("environment service not available")
	}

	response, err := proxyRemoteJSONInternal[base.ApiResponse[base.MessageResponse]](ctx, h.environmentService, input.EnvironmentID, http.MethodPut, "/api/environments/0/templates/variables", input.Body)
	if err != nil {
		return nil, err
	}

	return &updateGlobalVariablesOutput{Body: *response}, nil
}
