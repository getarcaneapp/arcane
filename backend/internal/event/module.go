// Package event owns persisted system events, manager ingestion, and event HTTP routes.
package event

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"
)

// Module owns event persistence and both of the domain's HTTP surfaces.
type Module struct {
	service *EventService
}

// New assembles the event routes around its service.
func New(service *EventService) *Module {
	return &Module{service: service}
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
func (m *Module) RegisterAgentRoutes(group *echo.Group, resolveEnvironment func(context.Context, string) (string, error)) {
	if m == nil {
		RegisterAgentEventIngestion(group, nil, nil)
		return
	}
	RegisterAgentEventIngestion(group, m.service, resolveEnvironment)
}
