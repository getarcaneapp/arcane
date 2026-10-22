// Package variable owns global variables, environment materialization, and routes.
package variable

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service     *VariableService
	environment *environment.EnvironmentService
}

func New(service *VariableService, environmentService *environment.EnvironmentService) *Module {
	return &Module{service: service, environment: environmentService}
}

func (m *Module) Service() *VariableService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API, cfg *config.Config) {
	if cfg != nil && cfg.AgentMode {
		if m == nil {
			RegisterMaterializedVariables(api, nil, nil)
			return
		}
		RegisterMaterializedVariables(api, m.service, m.environment)
		return
	}
	if m == nil {
		RegisterVariables(api, nil, nil)
		return
	}
	RegisterVariables(api, m.service, m.environment)
}

func RegisterVariables(api huma.API, variableService *VariableService, environmentService *environment.EnvironmentService) {
	h := &VariableHandler{variableService: variableService, proxyRemoteJSON: environmentService.ProxyJSONRequest}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "listVariables",
		Method:      "GET",
		Path:        "/variables",
		Summary:     "List global variables",
		Description: "List all global variables with their environment scope (secret values are redacted)",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesRead, h.ListVariables)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "createVariable",
		Method:      "POST",
		Path:        "/variables",
		Summary:     "Create a global variable",
		Description: "Create a global variable scoped to all or specific environments",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesCreate, h.CreateVariable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "updateVariable",
		Method:      "PATCH",
		Path:        "/variables/{id}",
		Summary:     "Update a global variable",
		Description: "Update a global variable's key, value, secret flag, or environment scope",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesUpdate, h.UpdateVariable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "deleteVariable",
		Method:      "DELETE",
		Path:        "/variables/{id}",
		Summary:     "Delete a global variable",
		Description: "Delete a global variable and re-sync affected environments",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesDelete, h.DeleteVariable)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "syncVariables",
		Method:      "POST",
		Path:        "/variables/sync",
		Summary:     "Sync global variables",
		Description: "Push the effective global variable set to every environment now",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesSync, h.SyncVariables)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "getVariableSyncStatus",
		Method:      "GET",
		Path:        "/variables/sync",
		Summary:     "Get variable sync status",
		Description: "Get the last global-variable sync result per environment",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermVariablesRead, h.GetSyncStatus)
}

// RegisterMaterializedVariables registers the plaintext synchronization
// channel on agents. It must never be registered on a manager API because its
// response intentionally contains decrypted values for .env.global.
func RegisterMaterializedVariables(api huma.API, variableService *VariableService, environmentService *environment.EnvironmentService) {
	h := &VariableHandler{variableService: variableService, proxyRemoteJSON: environmentService.ProxyJSONRequest}

	huma.Register(api, huma.Operation{
		OperationID: "getGlobalVariables",
		Method:      "GET",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Get materialized variables",
		Description: "Get the materialized variable set for an environment. Managed via /variables on the manager.",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireSudo(api),
	}, h.GetMaterializedVariables)

	huma.Register(api, huma.Operation{
		OperationID: "updateGlobalVariables",
		Method:      "PUT",
		Path:        "/environments/{id}/templates/variables",
		Summary:     "Update materialized variables",
		Description: "Replace the materialized variable set for an environment. Managed via /variables on the manager.",
		Tags:        []string{"Variables"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequireSudo(api),
	}, h.UpdateMaterializedVariables)
}
