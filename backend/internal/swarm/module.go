// Package swarm owns Docker Swarm management and its HTTP routes.
package swarm

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
)

type Dependencies struct {
	Environment *environment.EnvironmentService
	Event       *event.EventService
	Config      *config.Config
}

type Module struct {
	Service *SwarmService
	deps    Dependencies
}

func New(service *SwarmService, deps Dependencies) *Module {
	return &Module{Service: service, deps: deps}
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterSwarm(api, nil, nil, nil, nil)
		return
	}
	RegisterSwarm(api, m.Service, m.deps.Environment, m.deps.Event, m.deps.Config)
}
