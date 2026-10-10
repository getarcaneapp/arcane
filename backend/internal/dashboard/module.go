// Package dashboard owns the dashboard snapshot assembled from the container, project, image,
// volume and vulnerability domains, the HTTP surface that serves it, and local inventory metrics.
package dashboard

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel"

	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

// Module is the dashboard domain's wiring seam: it owns the service and the
// handler, and mounts the domain's routes.
type Module struct {
	service *DashboardService
	handler *DashboardHandler
}

// New wires dashboard routes around an existing service and registers its inventory metrics once.
func New(service *DashboardService, environmentService *environment.EnvironmentService) *Module {
	if service != nil {
		if err := service.ObserveInventory(otel.Meter(tracing.InstrumentationName)); err != nil {
			otel.Handle(err)
		}
	}
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

func RegisterDashboard(api huma.API, dashboardService *DashboardService, environmentService *environment.EnvironmentService) {
	h := NewHandler(dashboardService, environmentService)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-dashboard",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/dashboard",
		Summary:     "Get dashboard snapshot",
		Description: "Returns the dashboard first-paint snapshot in a single response",
		Tags:        []string{"Dashboard"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDashboardRead, h.GetDashboard)
}
