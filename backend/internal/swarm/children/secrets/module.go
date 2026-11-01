package secrets

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

// RegisterRoutes registers the swarm secret endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-secrets",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/secrets",
			Summary:     "List swarm secrets",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSwarmRead,
		h.ListSecrets,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-secret",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/secrets/{secretId}",
			Summary:     "Get swarm secret",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetSecret,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "create-swarm-secret",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/secrets",
			Summary:     "Create swarm secret",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmSecrets,
		h.CreateSecret,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-secret",
			Method:      http.MethodPut,
			Path:        "/environments/{id}/swarm/secrets/{secretId}",
			Summary:     "Update swarm secret labels",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmSecrets,
		h.UpdateSecret,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-secret",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/secrets/{secretId}",
			Summary:     "Delete swarm secret",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmSecrets,
		h.DeleteSecret,
	)
}
