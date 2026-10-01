// Package dashboard owns the dashboard snapshot: the service that assembles it
// from the container, project, image, volume and vulnerability domains, and the
// HTTP surface that serves it.
package dashboard

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
)

// Module is the dashboard domain's wiring seam: it owns the service and the
// handler, and mounts the domain's routes.
type Module struct {
	service *DashboardService
	handler *DashboardHandler
}

// New wires dashboard routes around an existing service.
func New(service *DashboardService, environmentService *environment.EnvironmentService) *Module {
	return &Module{service: service, handler: NewHandler(service, environmentService)}
}

// Handler exposes the dashboard stream producer.
func (m *Module) Handler() *DashboardHandler {
	if m == nil {
		return nil
	}
	return m.handler
}

// Service exposes the dashboard service to collaborators that compose its
// producers, such as the multiplexed client stream.
func (m *Module) Service() *DashboardService {
	if m == nil {
		return nil
	}
	return m.service
}

// RegisterRoutes mounts the dashboard endpoints. A nil module still registers,
// so OpenAPI spec generation can discover the routes without a service graph.
func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		RegisterDashboard(api, nil, nil)
		return
	}
	RegisterDashboard(api, m.service, m.handler.environmentService)
}
