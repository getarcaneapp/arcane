package configs

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterRoutes registers the swarm config endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-configs",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/configs",
			Summary:     "List swarm configs",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSwarmRead,
		h.ListConfigs,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-config",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/configs/{configId}",
			Summary:     "Get swarm config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "create-swarm-config",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/configs",
			Summary:     "Create swarm config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigs,
		h.CreateConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-config",
			Method:      http.MethodPut,
			Path:        "/environments/{id}/swarm/configs/{configId}",
			Summary:     "Update swarm config labels",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigs,
		h.UpdateConfig,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-config",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/configs/{configId}",
			Summary:     "Delete swarm config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmConfigs,
		h.DeleteConfig,
	)
}
