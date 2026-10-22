// Package template owns compose templates and the template HTTP surface.
package template

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service *TemplateService
}

func New(service *TemplateService) *Module {
	return &Module{service: service}
}

func (m *Module) Service() *TemplateService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterTemplates(api, nil)
		return
	}
	RegisterTemplates(api, m.service)
}

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
		Method:      "PATCH",
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

	middleware.RegisterWithActionPermissions(api, huma.Operation{
		OperationID: "templateAction",
		Method:      "POST",
		Path:        "/templates/{id}",
		Summary:     "Download a template",
		Description: "Download a remote template to local storage",
		Tags:        []string{"Templates"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, "action", map[string]string{
		"download": authz.PermTemplatesCreate,
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
		Method:      "PATCH",
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
