package nodes

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

// RegisterRoutes registers the swarm node endpoints.
func RegisterRoutes(api huma.API, h *Handler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-nodes",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/nodes",
			Summary:     "List swarm nodes",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.ListNodes,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-node",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}",
			Summary:     "Get swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetNode,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-node-agent-deployment",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}/agent/deployment",
			Summary:     "Get swarm node agent deployment snippets",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.GetNodeAgentDeployment,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "reconcile-swarm-node-agents",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/nodes/agents/reconcile",
			Summary:     "Reconcile swarm node agent bindings",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.ReconcileNodeAgents,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "put-swarm-node-agent-binding",
			Method:      http.MethodPut,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}/agent/binding",
			Summary:     "Attach a visible environment to a swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.PutNodeAgentBinding,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-node-agent-binding",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}/agent/binding",
			Summary:     "Detach a visible environment from a swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.DeleteNodeAgentBinding,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-node-agent-deployment",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}/agent/deployment",
			Summary:     "Remove a dedicated swarm node agent registration",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.DeleteNodeAgentDeployment,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-node",
			Method:      http.MethodPatch,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}",
			Summary:     "Update swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.UpdateNode,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "delete-swarm-node",
			Method:      http.MethodDelete,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}",
			Summary:     "Delete swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmNodes,
		h.DeleteNode,
	)
	middleware.RegisterWithActionPermissions(
		api,
		huma.Operation{
			OperationID: "swarm-node-action",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}",
			Summary:     "Promote or demote swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"promote": authz.PermSwarmNodes,
			"demote":  authz.PermSwarmNodes,
		},
		h.RunNodeAction,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-node-tasks",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/nodes/{nodeId}/tasks",
			Summary:     "List tasks for a swarm node",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.ListNodeTasks,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-node-identity",
			Method:      http.MethodGet,
			Path:        "/swarm/identity",
			Summary:     "Get local swarm node identity",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetNodeIdentity,
	)

	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-join-candidates",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/join/candidates",
			Summary:     "List environments available for Easy Join",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmJoin,
		h.GetJoinCandidates,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "join-swarm-environments",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/join/environments",
			Summary:     "Join environments to a swarm",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmJoin,
		h.JoinEnvironments,
	)
}
