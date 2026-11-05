package stacks

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
)

// RegisterRoutes registers the swarm stack endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-stacks",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/stacks",
			Summary:     "List swarm stacks",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
			Metadata: map[string]any{authz.MetaEnvironmentAggregate: true},
		},
		authz.PermSwarmStacksList,
		h.ListStacks,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "deploy-swarm-stack",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/stacks",
			Summary:     "Deploy swarm stack",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksCreate,
		h.DeployStack,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-stack",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/stacks/{name}",
			Summary:     "Get swarm stack",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksRead,
		h.GetStack,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-stack-source",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/stacks/{name}/source",
			Summary:     "Get swarm stack source",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksRead,
		h.GetStackSource,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-stack-source",
			Method:      http.MethodPatch,
			Path:        "/environments/{id}/swarm/stacks/{name}/source",
			Summary:     "Update swarm stack source",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksUpdate,
		h.UpdateStackSource,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-stack",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/stacks/{name}",
			Summary:     "Delete swarm stack",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksDelete,
		h.DeleteStack,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "down-swarm-stack",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/stacks/{name}/down",
			Summary:     "Bring down swarm stack",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksUpdate,
		h.DownStack,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-stack-services",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/stacks/{name}/services",
			Summary:     "List swarm stack services",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksRead,
		h.ListStackServices,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-stack-tasks",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/stacks/{name}/tasks",
			Summary:     "List swarm stack tasks",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksRead,
		h.ListStackTasks,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "render-swarm-stack-config",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/stacks/config/render",
			Summary:     "Render/validate swarm stack config",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmStacksCreate,
		h.RenderStackConfig,
	)
}
