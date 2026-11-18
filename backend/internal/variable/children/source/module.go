package source

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, h *SecretSourceHandler) {
	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listSecretSourcePresets",
		Method:      http.MethodGet,
		Path:        "/variables/source-presets",
		Summary:     "List secret source presets",
		Description: "List the built-in secret source definitions with their form metadata",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesList, h.ListPresets)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "validateSecretSourceDefinition",
		Method:      http.MethodPost,
		Path:        "/variables/source-presets/validate",
		Summary:     "Validate a secret source definition",
		Description: "Parse and validate a custom secret source definition YAML",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesRead, h.ValidateDefinition)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listSecretSources",
		Method:      http.MethodGet,
		Path:        "/variables/sources",
		Summary:     "List secret sources",
		Description: "List external secret sources (credentials are never returned)",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesList, h.ListSecretSources)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createSecretSource",
		Method:      http.MethodPost,
		Path:        "/variables/sources",
		Summary:     "Create a secret source",
		Description: "Create an external secret source, fetch it, and re-sync variables",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesCreate, h.CreateSecretSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "testSecretSource",
		Method:      http.MethodPost,
		Path:        "/variables/sources/test",
		Summary:     "Test a secret source",
		Description: "Fetch a saved or unsaved secret source and return only the key names",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesRefresh, h.TestSecretSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getSecretSource",
		Method:      http.MethodGet,
		Path:        "/variables/sources/{id}",
		Summary:     "Get a secret source",
		Description: "Get one external secret source and its fetch status",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesRead, h.GetSecretSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateSecretSource",
		Method:      http.MethodPatch,
		Path:        "/variables/sources/{id}",
		Summary:     "Update a secret source",
		Description: "Update submitted fields of a secret source; omitted credential keys are kept",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesUpdate, h.UpdateSecretSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteSecretSource",
		Method:      http.MethodDelete,
		Path:        "/variables/sources/{id}",
		Summary:     "Delete a secret source",
		Description: "Delete a secret source and re-sync variables",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesDelete, h.DeleteSecretSource)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "refreshSecretSource",
		Method:      http.MethodPost,
		Path:        "/variables/sources/{id}/refresh",
		Summary:     "Refresh a secret source",
		Description: "Fetch a secret source now and re-sync variables",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermSecretSourcesRefresh, h.RefreshSecretSource)
}
