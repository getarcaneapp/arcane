package swarm

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"

	"context"
	"log/slog"
	"maps"
	"net/http"
	"strings"

	"emperror.dev/errors"
	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/pagination"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/base"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
)

type SwarmHandler struct {
	swarmService       *SwarmService
	environmentService *environment.EnvironmentService
	eventService       *event.EventService
	cfg                *config.Config
}

type ListSwarmServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type CreateSwarmServiceInput struct {
	EnvironmentID string                          `path:"id" doc:"Environment ID"`
	Body          swarmtypes.ServiceCreateRequest `doc:"Service creation request"`
}

type UpdateSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarmtypes.ServiceUpdateRequest
}

type DeleteSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type ListSwarmServiceTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type RollbackSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type ScaleSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarmtypes.ServiceScaleRequest
}

type ListSwarmNodesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type GetSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type GetSwarmNodeAgentDeploymentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          struct {
		Rotate bool `json:"rotate,omitempty" doc:"Rotate the environment token before generating snippets"`
	}
}

type SwarmNodeAgentDeployment struct {
	environment.DeploymentSnippet

	EnvironmentID string                     `json:"environmentId"`
	Agent         swarmtypes.NodeAgentStatus `json:"agent"`
}

type ReconcileSwarmNodeAgentsInput struct {
	Body          swarmtypes.NodeAgentReconcileRequest
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type PutSwarmNodeAgentBindingInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          swarmtypes.NodeAgentBindingRequest
}

type DeleteSwarmNodeAgentBindingInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type DeleteSwarmNodeAgentDeploymentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type GetSwarmNodeIdentityInput struct{}

type UpdateSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          swarmtypes.NodeUpdateRequest
}

type DeleteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Force         bool   `query:"force" default:"false" doc:"Force node removal"`
}

type PromoteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type DemoteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type ListSwarmNodeTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmStacksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type DeploySwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.StackDeployRequest
}

type GetSwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type GetSwarmStackSourceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type UpdateSwarmStackSourceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
	Body          swarmtypes.StackSourceUpdateRequest
}

type DeleteSwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type ListSwarmStackServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmStackTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type RenderSwarmStackConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.StackRenderConfigRequest
}

type GetSwarmInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type InitSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmInitRequest
}

type JoinSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmJoinRequest
}

type GetSwarmJoinCandidatesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type JoinSwarmEnvironmentsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmJoinEnvironmentsRequest
}

type LeaveSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmLeaveRequest
}

type UnlockSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmUnlockRequest
}

type GetSwarmUnlockKeyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmJoinTokensInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type RotateSwarmJoinTokensInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmRotateJoinTokensRequest
}

type UpdateSwarmSpecInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmUpdateRequest
}

type ListSwarmConfigsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
}

type CreateSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.ConfigCreateRequest
}

type DeleteSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
}

type ListSwarmSecretsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type CreateSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SecretCreateRequest
}

type DeleteSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

// RegisterSwarm registers the Docker Swarm HTTP operations on the provided Huma API.
//
// It wires a SwarmHandler with the supplied services and publishes the full
// swarm route set for services, nodes, tasks, stacks, lifecycle operations,
// configs, and secrets.
//
// api is the Huma API instance that receives the swarm operations.
// swarmSvc provides the underlying swarm business logic.
// environmentSvc provides environment and agent-deployment helpers used by node endpoints.
// eventSvc records audit events for mutating operations when available.
// cfg provides application configuration needed by deployment-snippet endpoints.
func RegisterSwarm(api huma.API, swarmSvc *SwarmService, environmentSvc *environment.EnvironmentService, eventSvc *event.EventService, cfg *config.Config) {
	h := &SwarmHandler{
		swarmService:       swarmSvc,
		environmentService: environmentSvc,
		eventService:       eventSvc,
		cfg:                cfg,
	}

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-services", Method: http.MethodGet, Path: "/environments/{id}/swarm/services", Summary: "List swarm services", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListServices)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-service", Method: http.MethodGet, Path: "/environments/{id}/swarm/services/{serviceId}", Summary: "Get swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetService)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "create-swarm-service", Method: http.MethodPost, Path: "/environments/{id}/swarm/services", Summary: "Create swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmServices, h.CreateService)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-swarm-service", Method: http.MethodPut, Path: "/environments/{id}/swarm/services/{serviceId}", Summary: "Update swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmServices, h.UpdateService)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-service", Method: http.MethodDelete, Path: "/environments/{id}/swarm/services/{serviceId}", Summary: "Delete swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmServices, h.DeleteService)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-service-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/services/{serviceId}/tasks", Summary: "List tasks for a swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListServiceTasks)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "rollback-swarm-service", Method: http.MethodPost, Path: "/environments/{id}/swarm/services/{serviceId}/rollback", Summary: "Rollback a swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmServices, h.RollbackService)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "scale-swarm-service", Method: http.MethodPost, Path: "/environments/{id}/swarm/services/{serviceId}/scale", Summary: "Scale a swarm service", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmServices, h.ScaleService)

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-nodes", Method: http.MethodGet, Path: "/environments/{id}/swarm/nodes", Summary: "List swarm nodes", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListNodes)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-node", Method: http.MethodGet, Path: "/environments/{id}/swarm/nodes/{nodeId}", Summary: "Get swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetNode)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-node-agent-deployment", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/{nodeId}/agent/deployment", Summary: "Get swarm node agent deployment snippets", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.GetNodeAgentDeployment)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "reconcile-swarm-node-agents", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/agents/reconcile", Summary: "Reconcile swarm node agent bindings", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.ReconcileNodeAgents)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "put-swarm-node-agent-binding", Method: http.MethodPut, Path: "/environments/{id}/swarm/nodes/{nodeId}/agent/binding", Summary: "Attach a visible environment to a swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.PutNodeAgentBinding)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-node-agent-binding", Method: http.MethodDelete, Path: "/environments/{id}/swarm/nodes/{nodeId}/agent/binding", Summary: "Detach a visible environment from a swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.DeleteNodeAgentBinding)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-node-agent-deployment", Method: http.MethodDelete, Path: "/environments/{id}/swarm/nodes/{nodeId}/agent/deployment", Summary: "Remove a dedicated swarm node agent registration", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.DeleteNodeAgentDeployment)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-swarm-node", Method: http.MethodPatch, Path: "/environments/{id}/swarm/nodes/{nodeId}", Summary: "Update swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.UpdateNode)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-node", Method: http.MethodDelete, Path: "/environments/{id}/swarm/nodes/{nodeId}", Summary: "Delete swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.DeleteNode)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "promote-swarm-node", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/{nodeId}/promote", Summary: "Promote swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.PromoteNode)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "demote-swarm-node", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/{nodeId}/demote", Summary: "Demote swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmNodes, h.DemoteNode)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-node-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/nodes/{nodeId}/tasks", Summary: "List tasks for a swarm node", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListNodeTasks)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-node-identity", Method: http.MethodGet, Path: "/swarm/node-identity", Summary: "Get local swarm node identity", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetNodeIdentity)

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/tasks", Summary: "List swarm tasks", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListTasks)

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-stacks", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks", Summary: "List swarm stacks", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListStacks)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "deploy-swarm-stack", Method: http.MethodPost, Path: "/environments/{id}/swarm/stacks", Summary: "Deploy swarm stack", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmStacks, h.DeployStack)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-stack", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}", Summary: "Get swarm stack", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetStack)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-stack-source", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}/source", Summary: "Get swarm stack source", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmStacks, h.GetStackSource)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-swarm-stack-source", Method: http.MethodPut, Path: "/environments/{id}/swarm/stacks/{name}/source", Summary: "Update swarm stack source", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmStacks, h.UpdateStackSource)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-stack", Method: http.MethodDelete, Path: "/environments/{id}/swarm/stacks/{name}", Summary: "Delete swarm stack", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmStacks, h.DeleteStack)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-stack-services", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}/services", Summary: "List swarm stack services", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListStackServices)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-stack-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}/tasks", Summary: "List swarm stack tasks", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListStackTasks)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "render-swarm-stack-config", Method: http.MethodPost, Path: "/environments/{id}/swarm/stacks/config/render", Summary: "Render/validate swarm stack config", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.RenderStackConfig)

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-status", Method: http.MethodGet, Path: "/environments/{id}/swarm/status", Summary: "Get swarm status", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetSwarmStatus)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-info", Method: http.MethodGet, Path: "/environments/{id}/swarm/info", Summary: "Get swarm info", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetSwarmInfo)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "init-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/init", Summary: "Initialize swarm", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmInit, h.InitSwarm)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "join-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/join", Summary: "Join swarm", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmJoin, h.JoinSwarm)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-join-candidates", Method: http.MethodGet, Path: "/environments/{id}/swarm/join-candidates", Summary: "List environments available for Easy Join", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmJoin, h.GetJoinCandidates)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "join-swarm-environments", Method: http.MethodPost, Path: "/environments/{id}/swarm/join-environments", Summary: "Join environments to a swarm", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmJoin, h.JoinEnvironments)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "leave-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/leave", Summary: "Leave swarm", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmLeave, h.LeaveSwarm)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "unlock-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/unlock", Summary: "Unlock swarm", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmUnlock, h.UnlockSwarm)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-unlock-key", Method: http.MethodGet, Path: "/environments/{id}/swarm/unlock-key", Summary: "Get swarm unlock key", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmUnlock, h.GetUnlockKey)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-join-tokens", Method: http.MethodGet, Path: "/environments/{id}/swarm/join-tokens", Summary: "Get swarm join tokens", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmUnlock, h.GetJoinTokens)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "rotate-swarm-join-tokens", Method: http.MethodPost, Path: "/environments/{id}/swarm/join-tokens/rotate", Summary: "Rotate swarm join tokens", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmUnlock, h.RotateJoinTokens)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "update-swarm-spec", Method: http.MethodPut, Path: "/environments/{id}/swarm/spec", Summary: "Update swarm spec", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmSpec, h.UpdateSwarmSpec)

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-configs", Method: http.MethodGet, Path: "/environments/{id}/swarm/configs", Summary: "List swarm configs", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListConfigs)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-config", Method: http.MethodGet, Path: "/environments/{id}/swarm/configs/{configId}", Summary: "Get swarm config", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetConfig)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "create-swarm-config", Method: http.MethodPost, Path: "/environments/{id}/swarm/configs", Summary: "Create swarm config", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmConfigs, h.CreateConfig)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-config", Method: http.MethodDelete, Path: "/environments/{id}/swarm/configs/{configId}", Summary: "Delete swarm config", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmConfigs, h.DeleteConfig)

	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "list-swarm-secrets", Method: http.MethodGet, Path: "/environments/{id}/swarm/secrets", Summary: "List swarm secrets", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.ListSecrets)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "get-swarm-secret", Method: http.MethodGet, Path: "/environments/{id}/swarm/secrets/{secretId}", Summary: "Get swarm secret", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmRead, h.GetSecret)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "create-swarm-secret", Method: http.MethodPost, Path: "/environments/{id}/swarm/secrets", Summary: "Create swarm secret", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmSecrets, h.CreateSecret)
	middleware.RegisterWithPermission(api, huma.Operation{OperationID: "delete-swarm-secret", Method: http.MethodDelete, Path: "/environments/{id}/swarm/secrets/{secretId}", Summary: "Delete swarm secret", Tags: []string{"Swarm"}, Security: handlerutil.DefaultOperationSecurity()}, authz.PermSwarmSecrets, h.DeleteSecret)
}

// ListServices lists swarm services for an environment and returns a paginated response.
//
// It normalizes the search, sort, and pagination fields from input, delegates
// the lookup to the swarm service, and returns an empty slice instead of nil
// when no services are found.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies the environment ID plus optional search, sorting, and pagination values.
//
// Returns a successful response containing service summaries and pagination metadata.
// Returns an HTTP-shaped error if the swarm service is unavailable or if the
// underlying swarm lookup fails.
func (h *SwarmHandler) ListServices(ctx context.Context, input *ListSwarmServicesInput) (*handlerutil.Page[swarmtypes.ServiceSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListServicesPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to list swarm services").Error())
	}
	if items == nil {
		items = []swarmtypes.ServiceSummary{}
	}

	return &handlerutil.Page[swarmtypes.ServiceSummary]{Body: base.Paginated[swarmtypes.ServiceSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// GetService returns detailed information for a single swarm service.
//
// It loads the service by ID through the swarm service and converts lookup
// failures into the HTTP errors expected by the API.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and the swarm service to inspect.
//
// Returns a successful response containing the service inspection payload.
// Returns `404 Not Found` when the service does not exist and other mapped HTTP
// errors when the inspection fails.
func (h *SwarmHandler) GetService(ctx context.Context, input *GetSwarmServiceInput) (*handlerutil.Out[swarmtypes.ServiceInspect], error) {
	service, err := h.swarmService.GetService(ctx, input.ServiceID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound(errors.WithMessage(err, "Swarm service not found").Error())
		}
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm service not found").Error())
	}

	return &handlerutil.Out[swarmtypes.ServiceInspect]{Body: base.ApiResponse[swarmtypes.ServiceInspect]{Success: true, Data: *service}}, nil
}

// CreateService creates a new swarm service in the target environment.
//
// It requires admin privileges, forwards the create request to the swarm
// service, and records an audit event after a successful mutation.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input contains the environment ID and the requested service specification.
//
// Returns a successful response containing the created service ID and any Docker warnings.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when validation or creation fails.
func (h *SwarmHandler) CreateService(ctx context.Context, input *CreateSwarmServiceInput) (*handlerutil.Out[swarmtypes.ServiceCreateResponse], error) {
	resp, err := h.swarmService.CreateService(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to create swarm service").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.create", "swarm_service", resp.ID, "", map[string]any{"serviceId": resp.ID})

	return &handlerutil.Out[swarmtypes.ServiceCreateResponse]{Body: base.ApiResponse[swarmtypes.ServiceCreateResponse]{Success: true, Data: *resp}}, nil
}

// UpdateService updates an existing swarm service.
//
// It requires admin privileges, submits the requested versioned update to the
// swarm service, and emits an audit event when the update succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the service to update and provides the replacement specification and options.
//
// Returns a successful response containing any Docker warnings.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the update request is invalid or the underlying update fails.
func (h *SwarmHandler) UpdateService(ctx context.Context, input *UpdateSwarmServiceInput) (*handlerutil.Out[swarmtypes.ServiceUpdateResponse], error) {
	resp, err := h.swarmService.UpdateService(ctx, input.ServiceID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to update swarm service").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.update", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[swarmtypes.ServiceUpdateResponse]{Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

// DeleteService removes a swarm service.
//
// It requires admin privileges, asks the swarm service to remove the service,
// translates missing-service conditions to `404 Not Found`, and records an
// audit event after removal.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and service to remove.
//
// Returns a successful response with a confirmation message.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the service does not exist, or another mapped HTTP error when removal fails.
func (h *SwarmHandler) DeleteService(ctx context.Context, input *DeleteSwarmServiceInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RemoveService(ctx, input.ServiceID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound(errors.WithMessage(err, "Swarm service not found").Error())
		}
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to remove swarm service").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.delete", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm service removed successfully"}}}, nil
}

// ListServiceTasks lists tasks belonging to a specific swarm service.
//
// It applies the requested search, sort, and pagination values, delegates the
// lookup to the swarm service, and normalizes nil task slices to empty arrays.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the service and supplies optional filtering and pagination fields.
//
// Returns a paginated list of task summaries for the service.
// Returns a mapped HTTP error when the swarm task lookup fails.
func (h *SwarmHandler) ListServiceTasks(ctx context.Context, input *ListSwarmServiceTasksInput) (*handlerutil.Page[swarmtypes.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListServiceTasksPaginated(ctx, input.ServiceID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to list swarm tasks").Error())
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &handlerutil.Page[swarmtypes.TaskSummary]{Body: base.Paginated[swarmtypes.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// RollbackService requests a server-side rollback for a swarm service.
//
// It requires admin privileges, delegates the rollback to the swarm service,
// and records an audit event describing the mutation.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and service to roll back.
//
// Returns a successful response containing any warnings reported by Docker.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the rollback cannot be performed.
func (h *SwarmHandler) RollbackService(ctx context.Context, input *RollbackSwarmServiceInput) (*handlerutil.Out[swarmtypes.ServiceUpdateResponse], error) {
	resp, err := h.swarmService.RollbackService(ctx, input.ServiceID)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to update swarm service").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.rollback", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &handlerutil.Out[swarmtypes.ServiceUpdateResponse]{Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

// ScaleService changes the replica count of a swarm service.
//
// It requires admin privileges, forwards the requested replica count to the
// swarm service, and records the new replica target in the audit metadata.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the service and supplies the desired replica count.
//
// Returns a successful response containing any warnings reported by Docker.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when scaling is invalid or the update fails.
func (h *SwarmHandler) ScaleService(ctx context.Context, input *ScaleSwarmServiceInput) (*handlerutil.Out[swarmtypes.ServiceUpdateResponse], error) {
	resp, err := h.swarmService.ScaleService(ctx, input.ServiceID, input.Body.Replicas)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to update swarm service").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.scale", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID, "replicas": input.Body.Replicas})

	return &handlerutil.Out[swarmtypes.ServiceUpdateResponse]{Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
}

// ListNodes lists swarm nodes for an environment and returns a paginated response.
//
// It applies the requested search, sort, and pagination values and guarantees a
// non-nil node slice in the response body.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies the environment ID plus optional filtering and pagination values.
//
// Returns a paginated list of node summaries.
// Returns a mapped HTTP error when node enumeration fails.
func (h *SwarmHandler) ListNodes(ctx context.Context, input *ListSwarmNodesInput) (*handlerutil.Page[swarmtypes.NodeSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListNodesPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to list swarm nodes").Error())
	}
	if items == nil {
		items = []swarmtypes.NodeSummary{}
	}

	return &handlerutil.Page[swarmtypes.NodeSummary]{Body: base.Paginated[swarmtypes.NodeSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// GetNode returns detailed information for a single swarm node.
//
// It loads the node through the swarm service and translates not-found
// conditions into the HTTP error returned by the API.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and swarm node to inspect.
//
// Returns a successful response containing the node summary.
// Returns `404 Not Found` when the node does not exist or another mapped HTTP
// error when the inspection fails.
func (h *SwarmHandler) GetNode(ctx context.Context, input *GetSwarmNodeInput) (*handlerutil.Out[swarmtypes.NodeSummary], error) {
	node, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound(errors.WithMessage(err, "Swarm node not found").Error())
		}
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm node not found").Error())
	}

	return &handlerutil.Out[swarmtypes.NodeSummary]{Body: base.ApiResponse[swarmtypes.NodeSummary]{Success: true, Data: *node}}, nil
}

// GetNodeAgentDeployment returns deployment snippets for attaching an Arcane Remote Environment to a swarm node.
//
// It ensures a visible node-bound Remote Environment exists for new
// deployments, reuses legacy hidden registrations when present, optionally
// rotates the environment token, generates the appropriate deployment snippets,
// and refreshes the node summary so the response includes the latest agent
// status.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and node and optionally requests token rotation.
//
// Returns deployment snippets, the backing environment ID, and the refreshed agent status.
// Returns an authorization error for non-admin callers, `401 Unauthorized`
// when the current user cannot be resolved, `404 Not Found` when the node does
// not exist, or `500 Internal Server Error` when environment provisioning or
// snippet generation fails.
func (h *SwarmHandler) GetNodeAgentDeployment(ctx context.Context, input *GetSwarmNodeAgentDeploymentInput) (*handlerutil.Out[SwarmNodeAgentDeployment], error) {
	node, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound(errors.WithMessage(err, "Swarm node not found").Error())
		}
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm node not found").Error())
	}

	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}

	env, apiKey, err := h.environmentService.EnsureSwarmNodeAgentEnvironment(
		ctx,
		input.EnvironmentID,
		input.NodeID,
		node.Hostname,
		user.ID,
		user.Username,
		input.Body.Rotate,
	)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	var snippets *environment.DeploymentSnippets
	if env.IsEdge {
		snippets, err = h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), apiKey, &edge.Config{
			EdgeMTLSMode:      h.cfg.EdgeMTLSMode,
			EdgeMTLSCAFile:    h.cfg.EdgeMTLSCAFile,
			EdgeMTLSAssetsDir: h.cfg.EdgeMTLSAssetsDir,
			AppURL:            h.cfg.GetAppURL(),
		})
	} else {
		snippets, err = h.environmentService.GenerateDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), apiKey)
	}
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	updatedNode, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[SwarmNodeAgentDeployment]{
		Body: base.ApiResponse[SwarmNodeAgentDeployment]{
			Success: true,
			Data: SwarmNodeAgentDeployment{
				DeploymentSnippet: environment.DeploymentSnippet{
					DockerRun:     snippets.DockerRun,
					DockerCompose: snippets.DockerCompose,
				},
				EnvironmentID: env.ID,
				Agent:         updatedNode.Agent,
			},
		},
	}, nil
}

// ReconcileNodeAgents verifies and persists unique visible-environment node bindings.
func (h *SwarmHandler) ReconcileNodeAgents(ctx context.Context, input *ReconcileSwarmNodeAgentsInput) (*handlerutil.Out[swarmtypes.NodeAgentReconcileResponse], error) {
	result, err := h.swarmService.ReconcileNodeAgents(ctx, input.EnvironmentID)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to reconcile swarm node agents")
	}
	return &handlerutil.Out[swarmtypes.NodeAgentReconcileResponse]{Body: base.ApiResponse[swarmtypes.NodeAgentReconcileResponse]{Success: true, Data: *result}}, nil
}

// PutNodeAgentBinding verifies and attaches an existing visible environment.
func (h *SwarmHandler) PutNodeAgentBinding(ctx context.Context, input *PutSwarmNodeAgentBindingInput) (*handlerutil.Out[swarmtypes.NodeSummary], error) {
	if _, err := h.swarmService.BindNodeAgent(ctx, input.EnvironmentID, input.NodeID, input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if input.Body.ReplaceDeployment {
		user, err := handlerutil.RequireUser(ctx)
		if err != nil {
			return nil, err
		}
		if err := h.environmentService.DeleteSwarmNodeAgentDeployment(ctx, input.EnvironmentID, input.NodeID, &user.ID, &user.Username); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
	}

	node, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to refresh swarm node binding")
	}
	return &handlerutil.Out[swarmtypes.NodeSummary]{Body: base.ApiResponse[swarmtypes.NodeSummary]{Success: true, Data: *node}}, nil
}

// DeleteNodeAgentBinding detaches the visible environment currently bound to a node.
func (h *SwarmHandler) DeleteNodeAgentBinding(ctx context.Context, input *DeleteSwarmNodeAgentBindingInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.environmentService.DetachSwarmNodeEnvironment(ctx, input.EnvironmentID, input.NodeID); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node environment detached"}}}, nil
}

// DeleteNodeAgentDeployment removes a dedicated hidden node-agent registration.
func (h *SwarmHandler) DeleteNodeAgentDeployment(ctx context.Context, input *DeleteSwarmNodeAgentDeploymentInput) (*handlerutil.Out[base.MessageResponse], error) {
	user, err := handlerutil.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.environmentService.DeleteSwarmNodeAgentDeployment(ctx, input.EnvironmentID, input.NodeID, &user.ID, &user.Username); err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Dedicated swarm node agent registration removed"}}}, nil
}

// GetNodeIdentity returns the swarm identity of the node serving the current request.
//
// It is used by edge agents and local nodes to report their swarm node ID,
// hostname, role, engine version, and swarm participation state.
//
// ctx carries request-scoped cancellation and auth context.
// The input value is unused because the endpoint has no parameters.
//
// Returns the local swarm node identity when it can be determined.
// Returns `500 Internal Server Error` when the swarm service is unavailable or
// identity discovery fails.
func (h *SwarmHandler) GetNodeIdentity(ctx context.Context, _ *GetSwarmNodeIdentityInput) (*handlerutil.Out[SwarmNodeIdentity], error) {
	identity, err := h.swarmService.GetLocalNodeIdentity(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &handlerutil.Out[SwarmNodeIdentity]{
		Body: base.ApiResponse[SwarmNodeIdentity]{
			Success: true,
			Data:    *identity,
		},
	}, nil
}

// UpdateNode updates mutable settings on a swarm node.
//
// It requires admin privileges, forwards the requested node changes to the
// swarm service, and records an audit event when the mutation succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to update and contains the requested changes.
//
// Returns a confirmation response when the update succeeds.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the node update fails.
func (h *SwarmHandler) UpdateNode(ctx context.Context, input *UpdateSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.UpdateNode(ctx, input.NodeID, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm node not found").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.update", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node updated successfully"}}}, nil
}

// DeleteNode removes a swarm node from the cluster.
//
// It requires admin privileges, supports forced removal when requested, and
// records the deletion parameters in the audit event metadata.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to remove and indicates whether removal should be forced.
//
// Returns a confirmation response when the node is removed.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the node cannot be removed.
func (h *SwarmHandler) DeleteNode(ctx context.Context, input *DeleteSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RemoveNode(ctx, input.NodeID, input.Force); err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm node not found").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.delete", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID, "force": input.Force})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node removed successfully"}}}, nil
}

// PromoteNode promotes a swarm worker to manager.
//
// It requires admin privileges, performs the promotion through the swarm
// service, and records an audit event after the role change completes.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to promote.
//
// Returns a confirmation response when the promotion succeeds.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the promotion fails.
func (h *SwarmHandler) PromoteNode(ctx context.Context, input *PromoteSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.PromoteNode(ctx, input.NodeID); err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm node not found").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.promote", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node promoted successfully"}}}, nil
}

// DemoteNode demotes a swarm manager to worker.
//
// It requires admin privileges, performs the demotion through the swarm
// service, and records an audit event after the role change completes.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the node to demote.
//
// Returns a confirmation response when the demotion succeeds.
// Returns an authorization error for non-admin callers or a mapped HTTP error
// when the demotion fails.
func (h *SwarmHandler) DemoteNode(ctx context.Context, input *DemoteSwarmNodeInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.DemoteNode(ctx, input.NodeID); err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Swarm node not found").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.demote", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node demoted successfully"}}}, nil
}

// ListNodeTasks lists tasks currently associated with a swarm node.
//
// It applies search, sort, and pagination inputs and normalizes nil task lists
// to empty arrays in the API response.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the node and provides optional filtering and pagination values.
//
// Returns a paginated list of node task summaries.
// Returns a mapped HTTP error when the underlying lookup fails.
func (h *SwarmHandler) ListNodeTasks(ctx context.Context, input *ListSwarmNodeTasksInput) (*handlerutil.Page[swarmtypes.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListNodeTasksPaginated(ctx, input.NodeID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to list swarm tasks").Error())
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &handlerutil.Page[swarmtypes.TaskSummary]{Body: base.Paginated[swarmtypes.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// ListTasks lists swarm tasks across the current environment.
//
// It applies the requested search, sort, and pagination fields and guarantees
// an empty task slice when no tasks are returned.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies optional filtering and pagination values.
//
// Returns a paginated task listing for the environment.
// Returns a mapped HTTP error when task enumeration fails.
func (h *SwarmHandler) ListTasks(ctx context.Context, input *ListSwarmTasksInput) (*handlerutil.Page[swarmtypes.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListTasksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to list swarm tasks").Error())
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &handlerutil.Page[swarmtypes.TaskSummary]{Body: base.Paginated[swarmtypes.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// ListStacks lists swarm stacks for the current environment.
//
// It applies search, sort, and pagination values supplied by the caller and
// returns an empty stack slice instead of nil when no stacks are present.
//
// ctx carries request-scoped cancellation and auth context.
// input supplies optional filtering and pagination values.
//
// Returns a paginated list of stack summaries.
// Returns a mapped HTTP error when stack enumeration fails.
func (h *SwarmHandler) ListStacks(ctx context.Context, input *ListSwarmStacksInput) (*handlerutil.Page[swarmtypes.StackSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListStacksPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to list swarm stacks").Error())
	}
	if items == nil {
		items = []swarmtypes.StackSummary{}
	}

	return &handlerutil.Page[swarmtypes.StackSummary]{Body: base.Paginated[swarmtypes.StackSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// DeployStack deploys or updates a swarm stack.
//
// It requires admin privileges, submits the stack deployment request to the
// swarm service, and records an audit event keyed by the stack name after the
// deployment succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the target environment and provides the stack deployment request body.
//
// Returns the deployment response reported by the swarm service.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when rendering, validation, or deployment fails.
func (h *SwarmHandler) DeployStack(ctx context.Context, input *DeploySwarmStackInput) (*handlerutil.Out[swarmtypes.StackDeployResponse], error) {
	resp, err := h.swarmService.DeployStack(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to deploy swarm stack").Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "stack.deploy", "swarm_stack", input.Body.Name, input.Body.Name, map[string]any{"stack": input.Body.Name})

	return &handlerutil.Out[swarmtypes.StackDeployResponse]{Body: base.ApiResponse[swarmtypes.StackDeployResponse]{Success: true, Data: *resp}}, nil
}

// GetStack returns detailed information for a specific swarm stack.
//
// It looks up the stack by name through the swarm service and maps missing
// stacks to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and stack name to inspect.
//
// Returns the stack inspection payload when the stack exists.
// Returns `404 Not Found` when the stack does not exist or another mapped HTTP
// error when inspection fails.
func (h *SwarmHandler) GetStack(ctx context.Context, input *GetSwarmStackInput) (*handlerutil.Out[swarmtypes.StackInspect], error) {
	stack, err := h.swarmService.GetStack(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm stack")
	}

	return &handlerutil.Out[swarmtypes.StackInspect]{Body: base.ApiResponse[swarmtypes.StackInspect]{Success: true, Data: *stack}}, nil
}

// GetStackSource returns the stored source content for a swarm stack.
//
// It requires admin privileges because stack source content can include
// sensitive configuration, and it maps missing stack sources to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and stack whose saved source should be loaded.
//
// Returns the stored compose and environment source for the stack.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// no saved source exists, or another mapped HTTP error when loading fails.
func (h *SwarmHandler) GetStackSource(ctx context.Context, input *GetSwarmStackSourceInput) (*handlerutil.Out[swarmtypes.StackSource], error) {
	source, err := h.swarmService.GetStackSource(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack source not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to load swarm stack source")
	}

	return &handlerutil.Out[swarmtypes.StackSource]{Body: base.ApiResponse[swarmtypes.StackSource]{Success: true, Data: *source}}, nil
}

// UpdateStackSource persists the saved compose and env source for a swarm
// stack and redeploys the stack so the edit takes effect on running services.
//
// It requires admin privileges because stack source content can include
// sensitive configuration. The stack name comes from the route, and the body
// contains the replacement source files to save.
func (h *SwarmHandler) UpdateStackSource(ctx context.Context, input *UpdateSwarmStackSourceInput) (*handlerutil.Out[swarmtypes.StackSource], error) {
	source, err := h.swarmService.UpdateStackSource(ctx, input.EnvironmentID, input.Name, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to update swarm stack source")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "stack.source.update", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return &handlerutil.Out[swarmtypes.StackSource]{Body: base.ApiResponse[swarmtypes.StackSource]{Success: true, Data: *source}}, nil
}

// DeleteStack removes a swarm stack and its managed resources.
//
// It requires admin privileges, delegates the removal to the swarm service,
// maps missing stacks to `404 Not Found`, and records an audit event after
// deletion completes.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and stack name to remove.
//
// Returns a confirmation response when the stack is removed.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the stack does not exist, or another mapped HTTP error when removal fails.
func (h *SwarmHandler) DeleteStack(ctx context.Context, input *DeleteSwarmStackInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RemoveStack(ctx, input.EnvironmentID, input.Name); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to remove swarm stack")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "stack.delete", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm stack removed successfully"}}}, nil
}

// ListStackServices lists services belonging to a swarm stack.
//
// It applies search, sort, and pagination options, ensures the response uses an
// empty slice instead of nil, and maps missing stacks to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the stack and provides optional filtering and pagination fields.
//
// Returns a paginated list of service summaries for the stack.
// Returns `404 Not Found` when the stack does not exist or another mapped HTTP
// error when the lookup fails.
func (h *SwarmHandler) ListStackServices(ctx context.Context, input *ListSwarmStackServicesInput) (*handlerutil.Page[swarmtypes.ServiceSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListStackServicesPaginated(ctx, input.Name, params)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to list swarm stack services")
	}
	if items == nil {
		items = []swarmtypes.ServiceSummary{}
	}

	return &handlerutil.Page[swarmtypes.ServiceSummary]{Body: base.Paginated[swarmtypes.ServiceSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// ListStackTasks lists tasks belonging to a swarm stack.
//
// It applies search, sort, and pagination options, ensures the response uses an
// empty slice instead of nil, and maps missing stacks to `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the stack and provides optional filtering and pagination fields.
//
// Returns a paginated list of task summaries for the stack.
// Returns `404 Not Found` when the stack does not exist or another mapped HTTP
// error when the lookup fails.
func (h *SwarmHandler) ListStackTasks(ctx context.Context, input *ListSwarmStackTasksInput) (*handlerutil.Page[swarmtypes.TaskSummary], error) {
	params := handlerutil.PaginationParams(input.Start, input.Limit, input.Sort, input.Order, input.Search)
	items, paginationResp, err := h.swarmService.ListStackTasksPaginated(ctx, input.Name, params)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to list swarm stack tasks")
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &handlerutil.Page[swarmtypes.TaskSummary]{Body: base.Paginated[swarmtypes.TaskSummary]{Success: true, Data: items, Pagination: handlerutil.PaginationResponse(paginationResp)}}, nil
}

// RenderStackConfig renders and validates a swarm stack configuration without deploying it.
//
// It delegates to the swarm service to parse the provided compose and
// environment content and returns the normalized render result.
//
// ctx carries request-scoped cancellation and auth context.
// input provides the stack render request body.
//
// Returns the rendered compose content together with discovered resource names.
// Returns a mapped HTTP error when parsing, interpolation, or rendering fails.
func (h *SwarmHandler) RenderStackConfig(ctx context.Context, input *RenderSwarmStackConfigInput) (*handlerutil.Out[swarmtypes.StackRenderConfigResponse], error) {
	resp, err := h.swarmService.RenderStackConfig(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to render swarm stack config")
	}

	return &handlerutil.Out[swarmtypes.StackRenderConfigResponse]{Body: base.ApiResponse[swarmtypes.StackRenderConfigResponse]{Success: true, Data: *resp}}, nil
}

// GetSwarmStatus returns the current swarm cluster metadata for an environment.
//
// It delegates to the swarm service to inspect the local swarm state and maps
// service-layer failures to the API's HTTP error model.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose swarm metadata should be returned.
//
// Returns the current swarm information when swarm mode is available.
// Returns a mapped HTTP error when swarm inspection fails.
func (h *SwarmHandler) GetSwarmStatus(ctx context.Context, input *GetSwarmStatusInput) (*handlerutil.Out[swarmtypes.RuntimeStatus], error) {
	enabled, err := h.swarmService.IsEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read swarm status")
	}

	return &handlerutil.Out[swarmtypes.RuntimeStatus]{
		Body: base.ApiResponse[swarmtypes.RuntimeStatus]{
			Success: true,
			Data:    swarmtypes.RuntimeStatus{Enabled: enabled},
		},
	}, nil
}

// GetSwarmInfo returns the current swarm cluster metadata for an environment.
//
// It delegates to the swarm service to inspect the local swarm state and maps
// service-layer failures to the API's HTTP error model.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose swarm metadata should be returned.
//
// Returns the current swarm information when swarm mode is available.
// Returns a mapped HTTP error when swarm inspection fails.
func (h *SwarmHandler) GetSwarmInfo(ctx context.Context, input *GetSwarmInfoInput) (*handlerutil.Out[swarmtypes.SwarmInfo], error) {
	info, err := h.swarmService.GetSwarmInfo(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, errors.WithMessage(err, "Failed to inspect swarm").Error())
	}

	return &handlerutil.Out[swarmtypes.SwarmInfo]{Body: base.ApiResponse[swarmtypes.SwarmInfo]{Success: true, Data: *info}}, nil
}

// InitSwarm initializes swarm mode on the target engine.
//
// It requires admin privileges, delegates the initialization request to the
// swarm service, and records an audit event that includes the created node ID.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the swarm initialization request body.
//
// Returns the initialized swarm node ID and any other initialization details.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when initialization fails.
func (h *SwarmHandler) InitSwarm(ctx context.Context, input *InitSwarmInput) (*handlerutil.Out[swarmtypes.SwarmInitResponse], error) {
	resp, err := h.swarmService.InitSwarm(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to initialize swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.init", "swarm", "cluster", "cluster", map[string]any{"nodeId": resp.NodeID})

	return &handlerutil.Out[swarmtypes.SwarmInitResponse]{Body: base.ApiResponse[swarmtypes.SwarmInitResponse]{Success: true, Data: *resp}}, nil
}

// JoinSwarm joins the target engine to an existing swarm cluster.
//
// It requires admin privileges, forwards the join request to the swarm service,
// and records the remote manager addresses in the audit metadata.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the join request body.
//
// Returns a confirmation response when the engine joins successfully.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the join operation fails.
func (h *SwarmHandler) JoinSwarm(ctx context.Context, input *JoinSwarmInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.JoinSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to join swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.join", "swarm", "cluster", "cluster", map[string]any{"remoteAddrs": input.Body.RemoteAddrs})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Joined swarm successfully"}}}, nil
}

// GetJoinCandidates lists enabled visible environments the caller can join.
func (h *SwarmHandler) GetJoinCandidates(ctx context.Context, input *GetSwarmJoinCandidatesInput) (*handlerutil.Out[[]swarmtypes.SwarmJoinCandidate], error) {
	if err := requireEasyJoinManagerPermissionsInternal(ctx, input.EnvironmentID); err != nil {
		return nil, err
	}
	nodes, _, err := h.swarmService.ListNodesPaginated(ctx, input.EnvironmentID, pagination.QueryParams{Limit: -1})
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to list Easy Join candidates")
	}
	boundEnvironmentIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.Agent.EnvironmentID != nil {
			boundEnvironmentIDs[*node.Agent.EnvironmentID] = struct{}{}
		}
	}

	permissions, _ := middleware.PermissionsFromContext(ctx)
	environments, err := h.environmentService.ListSwarmNodeCandidateEnvironments(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	candidates := make([]swarmtypes.SwarmJoinCandidate, 0, len(environments))
	for _, environment := range environments {
		if environment.ID == input.EnvironmentID || permissions == nil || !permissions.Allows(authz.PermSwarmJoin, environment.ID) {
			continue
		}
		if _, bound := boundEnvironmentIDs[environment.ID]; bound {
			continue
		}
		environmentType := "direct"
		if environment.IsEdge {
			environmentType = "edge"
		}
		candidates = append(candidates, swarmtypes.SwarmJoinCandidate{
			EnvironmentID:   environment.ID,
			EnvironmentName: environment.Name,
			EnvironmentType: environmentType,
			Status:          environment.Status,
		})
	}

	return &handlerutil.Out[[]swarmtypes.SwarmJoinCandidate]{Body: base.ApiResponse[[]swarmtypes.SwarmJoinCandidate]{Success: true, Data: candidates}}, nil
}

// JoinEnvironments performs Easy Join without returning manager join tokens.
func (h *SwarmHandler) JoinEnvironments(ctx context.Context, input *JoinSwarmEnvironmentsInput) (*handlerutil.Out[swarmtypes.SwarmJoinEnvironmentsResponse], error) {
	if err := requireEasyJoinManagerPermissionsInternal(ctx, input.EnvironmentID); err != nil {
		return nil, err
	}

	permissions, _ := middleware.PermissionsFromContext(ctx)
	for _, target := range input.Body.Targets {
		if target.EnvironmentID == input.EnvironmentID {
			return nil, huma.Error400BadRequest("selected swarm manager cannot also be a join target")
		}
		if target.Role != swarmtypes.SwarmJoinEnvironmentRoleWorker && target.Role != swarmtypes.SwarmJoinEnvironmentRoleManager {
			return nil, huma.Error400BadRequest("join target role must be worker or manager")
		}
		if permissions == nil || !permissions.Allows(authz.PermSwarmJoin, target.EnvironmentID) {
			return nil, huma.Error403Forbidden("swarm:join permission is required for every target environment")
		}
	}

	if len(input.Body.RemoteAddrs) == 0 {
		nodes, _, err := h.swarmService.ListNodesPaginated(ctx, input.EnvironmentID, pagination.QueryParams{Limit: -1})
		if err != nil {
			return nil, mapSwarmServiceError(err, "Failed to derive swarm manager addresses")
		}
		for _, node := range nodes {
			if node.ManagerAddress != "" {
				input.Body.RemoteAddrs = append(input.Body.RemoteAddrs, node.ManagerAddress)
			}
		}
	}

	result, err := h.swarmService.JoinEnvironments(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to join swarm environments")
	}
	return &handlerutil.Out[swarmtypes.SwarmJoinEnvironmentsResponse]{Body: base.ApiResponse[swarmtypes.SwarmJoinEnvironmentsResponse]{Success: true, Data: *result}}, nil
}

func requireEasyJoinManagerPermissionsInternal(ctx context.Context, environmentID string) error {
	permissions, _ := middleware.PermissionsFromContext(ctx)
	if permissions == nil || !permissions.Allows(authz.PermSwarmJoin, environmentID) {
		return huma.Error403Forbidden("swarm:join permission is required on the manager environment")
	}
	return nil
}

// LeaveSwarm removes the target engine from its current swarm cluster.
//
// It requires admin privileges, forwards the leave request to the swarm
// service, and records whether forced removal was requested.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the leave request body.
//
// Returns a confirmation response when the engine leaves successfully.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the leave operation fails.
func (h *SwarmHandler) LeaveSwarm(ctx context.Context, input *LeaveSwarmInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.LeaveSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to leave swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.leave", "swarm", "cluster", "cluster", map[string]any{"force": input.Body.Force})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Left swarm successfully"}}}, nil
}

// UnlockSwarm unlocks a swarm manager using the supplied unlock key.
//
// It requires admin privileges, delegates the unlock request to the swarm
// service, and emits an audit event after success.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the unlock request body.
//
// Returns a confirmation response when the manager is unlocked.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the unlock operation fails.
func (h *SwarmHandler) UnlockSwarm(ctx context.Context, input *UnlockSwarmInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.UnlockSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to unlock swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.unlock", "swarm", "cluster", "cluster", map[string]any{})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm unlocked successfully"}}}, nil
}

// GetUnlockKey returns the current swarm manager unlock key.
//
// It delegates to the swarm service and exposes the unlock key in the standard
// API response envelope.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose unlock key should be returned.
//
// Returns the current manager unlock key.
// Returns a mapped HTTP error when the unlock key cannot be retrieved.
func (h *SwarmHandler) GetUnlockKey(ctx context.Context, input *GetSwarmUnlockKeyInput) (*handlerutil.Out[swarmtypes.SwarmUnlockKeyResponse], error) {
	resp, err := h.swarmService.GetSwarmUnlockKey(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to get swarm unlock key")
	}

	return &handlerutil.Out[swarmtypes.SwarmUnlockKeyResponse]{Body: base.ApiResponse[swarmtypes.SwarmUnlockKeyResponse]{Success: true, Data: *resp}}, nil
}

// GetJoinTokens returns the current swarm worker and manager join tokens.
//
// It delegates to the swarm service and wraps the returned tokens in the
// standard API response shape.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose join tokens should be returned.
//
// Returns the current worker and manager join tokens.
// Returns a mapped HTTP error when token lookup fails.
func (h *SwarmHandler) GetJoinTokens(ctx context.Context, input *GetSwarmJoinTokensInput) (*handlerutil.Out[swarmtypes.SwarmJoinTokensResponse], error) {
	resp, err := h.swarmService.GetSwarmJoinTokens(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to get swarm join tokens")
	}

	return &handlerutil.Out[swarmtypes.SwarmJoinTokensResponse]{Body: base.ApiResponse[swarmtypes.SwarmJoinTokensResponse]{Success: true, Data: *resp}}, nil
}

// RotateJoinTokens rotates the swarm worker and or manager join tokens.
//
// It requires admin privileges, delegates the rotation request to the swarm
// service, and records which token classes were rotated.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the requested token-rotation flags.
//
// Returns a confirmation response when rotation succeeds.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when token rotation fails.
func (h *SwarmHandler) RotateJoinTokens(ctx context.Context, input *RotateSwarmJoinTokensInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RotateSwarmJoinTokens(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to rotate swarm join tokens")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.rotate_tokens", "swarm", "cluster", "cluster", map[string]any{"rotateWorker": input.Body.RotateWorkerToken, "rotateManager": input.Body.RotateManagerToken})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm join tokens rotated successfully"}}}, nil
}

// UpdateSwarmSpec updates the swarm cluster specification.
//
// It requires admin privileges, forwards the request to the swarm service, and
// records an audit event after the spec change succeeds.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the replacement swarm spec.
//
// Returns a confirmation response when the spec update succeeds.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the spec update fails.
func (h *SwarmHandler) UpdateSwarmSpec(ctx context.Context, input *UpdateSwarmSpecInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.UpdateSwarmSpec(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to update swarm spec")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.update_spec", "swarm", "cluster", "cluster", map[string]any{})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm spec updated successfully"}}}, nil
}

// ListConfigs lists swarm configs in the current environment.
//
// It delegates to the swarm service and normalizes nil config slices to empty
// arrays in the response body.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose configs should be listed.
//
// Returns the current swarm configs.
// Returns a mapped HTTP error when config enumeration fails.
func (h *SwarmHandler) ListConfigs(ctx context.Context, input *ListSwarmConfigsInput) (*handlerutil.Out[[]swarmtypes.ConfigSummary], error) {
	items, err := h.swarmService.ListConfigs(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to list swarm configs")
	}
	if items == nil {
		items = []swarmtypes.ConfigSummary{}
	}

	return &handlerutil.Out[[]swarmtypes.ConfigSummary]{Body: base.ApiResponse[[]swarmtypes.ConfigSummary]{Success: true, Data: items}}, nil
}

// GetConfig returns details for a single swarm config.
//
// It delegates to the swarm service and maps missing configs to
// `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and swarm config to inspect.
//
// Returns the config summary when the config exists.
// Returns `404 Not Found` when the config does not exist or another mapped HTTP
// error when inspection fails.
func (h *SwarmHandler) GetConfig(ctx context.Context, input *GetSwarmConfigInput) (*handlerutil.Out[swarmtypes.ConfigSummary], error) {
	cfg, err := h.swarmService.GetConfig(ctx, input.ConfigID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm config not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm config")
	}

	return &handlerutil.Out[swarmtypes.ConfigSummary]{Body: base.ApiResponse[swarmtypes.ConfigSummary]{Success: true, Data: *cfg}}, nil
}

// CreateConfig creates a new swarm config.
//
// It requires admin privileges, delegates the creation request to the swarm
// service, and records an audit event containing the created config ID and name.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the config specification.
//
// Returns the created config summary.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when validation or creation fails.
func (h *SwarmHandler) CreateConfig(ctx context.Context, input *CreateSwarmConfigInput) (*handlerutil.Out[swarmtypes.ConfigSummary], error) {
	cfg, err := h.swarmService.CreateConfig(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to create swarm config")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "config.create", "swarm_config", cfg.ID, cfg.Spec.Name, map[string]any{"configId": cfg.ID, "name": cfg.Spec.Name})

	return &handlerutil.Out[swarmtypes.ConfigSummary]{Body: base.ApiResponse[swarmtypes.ConfigSummary]{Success: true, Data: *cfg}}, nil
}

// DeleteConfig removes a swarm config.
//
// It requires admin privileges, delegates removal to the swarm service, maps
// missing configs to `404 Not Found`, and records an audit event on success.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the config to remove.
//
// Returns a confirmation response when the config is removed.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the config does not exist, or another mapped HTTP error when removal fails.
func (h *SwarmHandler) DeleteConfig(ctx context.Context, input *DeleteSwarmConfigInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RemoveConfig(ctx, input.ConfigID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm config not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to remove swarm config")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "config.delete", "swarm_config", input.ConfigID, "", map[string]any{"configId": input.ConfigID})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm config removed successfully"}}}, nil
}

// ListSecrets lists swarm secrets in the current environment.
//
// It delegates to the swarm service and normalizes nil secret slices to empty
// arrays in the response body.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment whose secrets should be listed.
//
// Returns the current swarm secrets.
// Returns a mapped HTTP error when secret enumeration fails.
func (h *SwarmHandler) ListSecrets(ctx context.Context, input *ListSwarmSecretsInput) (*handlerutil.Out[[]swarmtypes.SecretSummary], error) {
	items, err := h.swarmService.ListSecrets(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to list swarm secrets")
	}
	if items == nil {
		items = []swarmtypes.SecretSummary{}
	}

	return &handlerutil.Out[[]swarmtypes.SecretSummary]{Body: base.ApiResponse[[]swarmtypes.SecretSummary]{Success: true, Data: items}}, nil
}

// GetSecret returns details for a single swarm secret.
//
// It delegates to the swarm service and maps missing secrets to
// `404 Not Found`.
//
// ctx carries request-scoped cancellation and auth context.
// input identifies the environment and secret to inspect.
//
// Returns the secret summary when the secret exists.
// Returns `404 Not Found` when the secret does not exist or another mapped HTTP
// error when inspection fails.
func (h *SwarmHandler) GetSecret(ctx context.Context, input *GetSwarmSecretInput) (*handlerutil.Out[swarmtypes.SecretSummary], error) {
	secret, err := h.swarmService.GetSecret(ctx, input.SecretID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm secret not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm secret")
	}

	return &handlerutil.Out[swarmtypes.SecretSummary]{Body: base.ApiResponse[swarmtypes.SecretSummary]{Success: true, Data: *secret}}, nil
}

// CreateSecret creates a new swarm secret.
//
// It requires admin privileges, delegates the creation request to the swarm
// service, and records an audit event containing the created secret ID and name.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and contains the secret specification.
//
// Returns the created secret summary.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when validation or creation fails.
func (h *SwarmHandler) CreateSecret(ctx context.Context, input *CreateSwarmSecretInput) (*handlerutil.Out[swarmtypes.SecretSummary], error) {
	secret, err := h.swarmService.CreateSecret(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to create swarm secret")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "secret.create", "swarm_secret", secret.ID, secret.Spec.Name, map[string]any{"secretId": secret.ID, "name": secret.Spec.Name})

	return &handlerutil.Out[swarmtypes.SecretSummary]{Body: base.ApiResponse[swarmtypes.SecretSummary]{Success: true, Data: *secret}}, nil
}

// DeleteSecret removes a swarm secret.
//
// It requires admin privileges, delegates removal to the swarm service, maps
// missing secrets to `404 Not Found`, and records an audit event on success.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the secret to remove.
//
// Returns a confirmation response when the secret is removed.
// Returns an authorization error for non-admin callers, `404 Not Found` when
// the secret does not exist, or another mapped HTTP error when removal fails.
func (h *SwarmHandler) DeleteSecret(ctx context.Context, input *DeleteSwarmSecretInput) (*handlerutil.Out[base.MessageResponse], error) {
	if err := h.swarmService.RemoveSecret(ctx, input.SecretID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm secret not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to remove swarm secret")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "secret.delete", "swarm_secret", input.SecretID, "", map[string]any{"secretId": input.SecretID})

	return &handlerutil.Out[base.MessageResponse]{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm secret removed successfully"}}}, nil
}

// auditSwarmMutation writes an informational event for a completed swarm mutation.
//
// It enriches the event with the current user when available, normalizes blank
// environment IDs to the local environment, and logs a warning instead of
// failing the request when event creation is unsuccessful.
//
// ctx carries request-scoped cancellation and user context.
// environmentID identifies the environment associated with the mutation.
// action names the performed swarm action.
// resourceType classifies the mutated resource for the audit trail.
// resourceID identifies the mutated resource when one exists.
// resourceName provides a human-readable resource name when one exists.
// metadata supplies additional structured audit fields to attach to the event.
func (h *SwarmHandler) auditSwarmMutation(ctx context.Context, environmentID, action, resourceType, resourceID, resourceName string, metadata map[string]any) {
	if h.eventService == nil {
		return
	}

	var userID *string
	var username *string
	if user, ok := common.CurrentUserFromContext(ctx); ok {
		userID = new(user.ID)
		username = new(user.Username)
	}

	var resourceTypePtr *string
	if strings.TrimSpace(resourceType) != "" {
		resourceTypePtr = new(resourceType)
	}
	var resourceIDPtr *string
	if strings.TrimSpace(resourceID) != "" {
		resourceIDPtr = new(resourceID)
	}
	var resourceNamePtr *string
	if strings.TrimSpace(resourceName) != "" {
		resourceNamePtr = new(resourceName)
	}

	env := strings.TrimSpace(environmentID)
	if env == "" {
		env = "0"
	}
	envPtr := &env

	meta := database.JSON{"action": action}
	maps.Copy(meta, metadata)

	_, err := h.eventService.CreateEvent(ctx, event.CreateEventRequest{
		Type:          event.EventType("swarm." + action),
		Severity:      event.EventSeverityInfo,
		Title:         "Swarm operation: " + action,
		Description:   "Swarm operation '" + action + "' completed",
		ResourceType:  resourceTypePtr,
		ResourceID:    resourceIDPtr,
		ResourceName:  resourceNamePtr,
		UserID:        userID,
		Username:      username,
		EnvironmentID: envPtr,
		Metadata:      meta,
	})
	if err != nil {
		slog.WarnContext(ctx, "failed to audit swarm mutation", "action", action, "error", err)
	}
}

// mapSwarmServiceError converts swarm-service errors into Huma HTTP errors.
//
// It recognizes Arcane's swarm sentinel errors, common Docker error classes,
// and a small set of validation-like substrings before falling back to an
// internal-server-error response.
//
// err is the original service-layer error to translate.
// fallback is the generic message returned when no specific mapping applies.
//
// Returns an HTTP-shaped error suitable for returning from a Huma handler.
func mapSwarmServiceError(err error, fallback string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, common.ErrSwarmNotEnabled) {
		return huma.Error409Conflict("Swarm mode is not enabled")
	}
	if errors.Is(err, common.ErrSwarmManagerRequired) {
		return huma.Error403Forbidden("Swarm manager access required")
	}
	if errors.Is(err, common.ErrBadRequest) {
		return huma.Error400BadRequest(err.Error())
	}
	if errdefs.IsNotFound(err) {
		return huma.Error404NotFound(err.Error())
	}
	if errdefs.IsInvalidArgument(err) {
		return huma.Error400BadRequest(err.Error())
	}
	if errdefs.IsConflict(err) {
		return huma.Error409Conflict(err.Error())
	}
	errText := strings.ToLower(err.Error())
	if strings.Contains(errText, "required") || strings.Contains(errText, "invalid") {
		return huma.Error400BadRequest(err.Error())
	}
	return huma.Error500InternalServerError(fallback)
}
