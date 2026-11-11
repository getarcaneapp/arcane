package dashboard

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterDashboard(api huma.API, dashboardService *DashboardService) {
	h := &DashboardHandler{dashboardService: dashboardService}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-dashboard-summary",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/dashboard",
		Summary:     "Get dashboard summary",
		Description: "Get the dashboard sections for an environment in one response; each section carries its own error",
		Tags:        []string{"Dashboard"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Metadata:    map[string]any{authz.MetaEnvironmentAggregate: true},
	}, authz.PermContainersList, h.GetSummary)
}
