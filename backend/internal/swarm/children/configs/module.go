package configs

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-configs",
			Method:      http.MethodGet,
			Path:        "/agents/{id}/swarm/configs",
			Summary:     "List swarm configs",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSwarmConfigsList,
		h.ListConfigs,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-config",
			Method:      http.MethodGet,
			Path:        "/agents/{id}/swarm/configs/{configId}",
			Summary:     "Get swarm config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigsRead,
		h.GetConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "create-swarm-config",
			Method:      http.MethodPost,
			Path:        "/agents/{id}/swarm/configs",
			Summary:     "Create swarm config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigsCreate,
		h.CreateConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-config",
			Method:      http.MethodPut,
			Path:        "/agents/{id}/swarm/configs/{configId}",
			Summary:     "Update swarm config labels",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigsUpdate,
		h.UpdateConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-config",
			Method:      http.MethodDelete,
			Path:        "/agents/{id}/swarm/configs/{configId}",
			Summary:     "Delete swarm config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigsDelete,
		h.DeleteConfig,
	)
}
