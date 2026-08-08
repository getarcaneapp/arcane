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
	service *SwarmService
	deps    Dependencies
}

func New(service *SwarmService, deps Dependencies) *Module {
	return &Module{service: service, deps: deps}
}

func (m *Module) Service() *SwarmService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterSwarm(api, nil, nil, nil, nil)
		return
	}
	RegisterSwarm(api, m.service, m.deps.Environment, m.deps.Event, m.deps.Config)
}
