package services

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterRoutes registers the swarm service endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-services",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/services",
			Summary:     "List swarm services",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSwarmRead,
		h.ListServices,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-service",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/services/{serviceId}",
			Summary:     "Get swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetService,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "create-swarm-service",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/services",
			Summary:     "Create swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmServices,
		h.CreateService,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-service",
			Method:      http.MethodPut,
			Path:        "/environments/{id}/swarm/services/{serviceId}",
			Summary:     "Update swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmServices,
		h.UpdateService,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-service",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/services/{serviceId}",
			Summary:     "Delete swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmServices,
		h.DeleteService,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-service-tasks",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/services/{serviceId}/tasks",
			Summary:     "List tasks for a swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.ListServiceTasks,
	)
	middleware.RegisterWithActionPermissions(
		api,
		huma.Operation{
			OperationID: "swarm-service-action",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/services/{serviceId}",
			Summary:     "Rollback a swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"rollback": authz.PermSwarmServices,
		},
		h.RollbackService,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "scale-swarm-service",
			Method:      http.MethodPatch,
			Path:        "/environments/{id}/swarm/services/{serviceId}",
			Summary:     "Scale a swarm service",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmServices,
		h.ScaleService,
	)
}
