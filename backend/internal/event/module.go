// Package event owns persisted system events, manager ingestion, and event HTTP routes.
package event

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/labstack/echo/v5"
)

// Dependencies are the collaborators the event domain needs.
type Dependencies struct {
	DB         *database.DB
	Config     *config.Config
	HTTPClient *http.Client
}

// Module owns event persistence and both of the domain's HTTP surfaces.
type Module struct {
	service *EventService
	config  *config.Config
}

// New builds the event domain from its dependencies.
func New(deps Dependencies) *Module {
	return &Module{
		service: NewEventService(deps.DB, deps.Config, deps.HTTPClient),
		config:  deps.Config,
	}
}

// Service exposes event operations to collaborating domains.
func (m *Module) Service() *EventService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the typed event endpoints for runtime and schema discovery.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterEvents(api, nil)
		return
	}
	RegisterEvents(api, m.service)
}

// RegisterAgentRoutes mounts the token-authenticated direct-agent ingestion endpoint.
func (m *Module) RegisterAgentRoutes(group *echo.Group) {
	if m == nil {
		RegisterAgentEventIngestion(group, nil, nil)
		return
	}
	RegisterAgentEventIngestion(group, m.service, m.config)
}
