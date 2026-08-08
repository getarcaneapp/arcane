// Package variable owns global variables, environment materialization, and routes.
package variable

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
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
