// Package swarm owns Docker Swarm management and its HTTP routes.
package swarm

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
)

type Module struct {
	service     *SwarmService
	environment *environment.EnvironmentService
	event       *event.EventService
	config      *config.Config
}

func New(service *SwarmService, environmentService *environment.EnvironmentService, eventService *event.EventService, cfg *config.Config) *Module {
	return &Module{service: service, environment: environmentService, event: eventService, config: cfg}
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
	RegisterSwarm(api, m.service, m.environment, m.event, m.config)
}
