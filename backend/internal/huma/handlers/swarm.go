package handlers

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	humamw "github.com/getarcaneapp/arcane/backend/internal/huma/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	"github.com/getarcaneapp/arcane/types/base"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
)

type SwarmHandler struct {
	swarmService       *services.SwarmService
	environmentService *services.EnvironmentService
	eventService       *services.EventService
	cfg                *config.Config
}

type SwarmPaginatedResponse[T any] struct {
	Success    bool                    `json:"success"`
	Data       []T                     `json:"data"`
	Pagination base.PaginationResponse `json:"pagination"`
}

type ListSwarmServicesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmServicesOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.ServiceSummary]
}

type GetSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type GetSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceInspect]
}

type CreateSwarmServiceInput struct {
	EnvironmentID string                          `path:"id" doc:"Environment ID"`
	Body          swarmtypes.ServiceCreateRequest `doc:"Service creation request"`
}

type CreateSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceCreateResponse]
}

type UpdateSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarmtypes.ServiceUpdateRequest
}

type UpdateSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceUpdateResponse]
}

type DeleteSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type DeleteSwarmServiceOutput struct {
	Body base.ApiResponse[base.MessageResponse]
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

type ListSwarmServiceTasksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.TaskSummary]
}

type RollbackSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
}

type RollbackSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceUpdateResponse]
}

type ScaleSwarmServiceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ServiceID     string `path:"serviceId" doc:"Service ID"`
	Body          swarmtypes.ServiceScaleRequest
}

type ScaleSwarmServiceOutput struct {
	Body base.ApiResponse[swarmtypes.ServiceUpdateResponse]
}

type ListSwarmNodesInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmNodesOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.NodeSummary]
}

type GetSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type GetSwarmNodeOutput struct {
	Body base.ApiResponse[swarmtypes.NodeSummary]
}

type GetSwarmNodeAgentDeploymentInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          struct {
		Rotate bool `json:"rotate,omitempty" doc:"Rotate the environment token before generating snippets"`
	}
}

type SwarmNodeAgentDeployment struct {
	DeploymentSnippet
	EnvironmentID string                     `json:"environmentId"`
	Agent         swarmtypes.NodeAgentStatus `json:"agent"`
}

type GetSwarmNodeAgentDeploymentOutput struct {
	Body base.ApiResponse[SwarmNodeAgentDeployment]
}

type GetSwarmNodeIdentityInput struct{}

type GetSwarmNodeIdentityOutput struct {
	Body base.ApiResponse[services.SwarmNodeIdentity]
}

type UpdateSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Body          swarmtypes.NodeUpdateRequest
}

type UpdateSwarmNodeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DeleteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
	Force         bool   `query:"force" default:"false" doc:"Force node removal"`
}

type DeleteSwarmNodeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type PromoteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type PromoteSwarmNodeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type DemoteSwarmNodeInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	NodeID        string `path:"nodeId" doc:"Node ID"`
}

type DemoteSwarmNodeOutput struct {
	Body base.ApiResponse[base.MessageResponse]
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

type ListSwarmNodeTasksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.TaskSummary]
}

type ListSwarmTasksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmTasksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.TaskSummary]
}

type ListSwarmStacksInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Search        string `query:"search" doc:"Search query"`
	Sort          string `query:"sort" doc:"Column to sort by"`
	Order         string `query:"order" default:"asc" doc:"Sort direction (asc or desc)"`
	Start         int    `query:"start" default:"0" doc:"Start index for pagination"`
	Limit         int    `query:"limit" default:"20" doc:"Number of items per page"`
}

type ListSwarmStacksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.StackSummary]
}

type DeploySwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.StackDeployRequest
}

type DeploySwarmStackOutput struct {
	Body base.ApiResponse[swarmtypes.StackDeployResponse]
}

type GetSwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type GetSwarmStackOutput struct {
	Body base.ApiResponse[swarmtypes.StackInspect]
}

type GetSwarmStackSourceInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type GetSwarmStackSourceOutput struct {
	Body base.ApiResponse[swarmtypes.StackSource]
}

type DeleteSwarmStackInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Name          string `path:"name" doc:"Stack name"`
}

type DeleteSwarmStackOutput struct {
	Body base.ApiResponse[base.MessageResponse]
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

type ListSwarmStackServicesOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.ServiceSummary]
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

type ListSwarmStackTasksOutput struct {
	Body SwarmPaginatedResponse[swarmtypes.TaskSummary]
}

type RenderSwarmStackConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.StackRenderConfigRequest
}

type RenderSwarmStackConfigOutput struct {
	Body base.ApiResponse[swarmtypes.StackRenderConfigResponse]
}

type GetSwarmInfoInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmInfoOutput struct {
	Body base.ApiResponse[swarmtypes.SwarmInfo]
}

type GetSwarmStatusInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmStatusOutput struct {
	Body base.ApiResponse[swarmtypes.RuntimeStatus]
}

type InitSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmInitRequest
}

type InitSwarmOutput struct {
	Body base.ApiResponse[swarmtypes.SwarmInitResponse]
}

type JoinSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmJoinRequest
}

type JoinSwarmOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type LeaveSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmLeaveRequest
}

type LeaveSwarmOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type UnlockSwarmInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmUnlockRequest
}

type UnlockSwarmOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type GetSwarmUnlockKeyInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmUnlockKeyOutput struct {
	Body base.ApiResponse[swarmtypes.SwarmUnlockKeyResponse]
}

type GetSwarmJoinTokensInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetSwarmJoinTokensOutput struct {
	Body base.ApiResponse[swarmtypes.SwarmJoinTokensResponse]
}

type RotateSwarmJoinTokensInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmRotateJoinTokensRequest
}

type RotateSwarmJoinTokensOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type UpdateSwarmSpecInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SwarmUpdateRequest
}

type UpdateSwarmSpecOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type ListSwarmConfigsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ListSwarmConfigsOutput struct {
	Body base.ApiResponse[[]swarmtypes.ConfigSummary]
}

type GetSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
}

type GetSwarmConfigOutput struct {
	Body base.ApiResponse[swarmtypes.ConfigSummary]
}

type CreateSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.ConfigCreateRequest
}

type CreateSwarmConfigOutput struct {
	Body base.ApiResponse[swarmtypes.ConfigSummary]
}

type UpdateSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
	Body          swarmtypes.ConfigUpdateRequest
}

type UpdateSwarmConfigOutput struct {
	Body base.ApiResponse[swarmtypes.ConfigSummary]
}

type DeleteSwarmConfigInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	ConfigID      string `path:"configId" doc:"Config ID"`
}

type DeleteSwarmConfigOutput struct {
	Body base.ApiResponse[base.MessageResponse]
}

type ListSwarmSecretsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type ListSwarmSecretsOutput struct {
	Body base.ApiResponse[[]swarmtypes.SecretSummary]
}

type GetSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type GetSwarmSecretOutput struct {
	Body base.ApiResponse[swarmtypes.SecretSummary]
}

type CreateSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	Body          swarmtypes.SecretCreateRequest
}

type CreateSwarmSecretOutput struct {
	Body base.ApiResponse[swarmtypes.SecretSummary]
}

type UpdateSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
	Body          swarmtypes.SecretUpdateRequest
}

type UpdateSwarmSecretOutput struct {
	Body base.ApiResponse[swarmtypes.SecretSummary]
}

type DeleteSwarmSecretInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
	SecretID      string `path:"secretId" doc:"Secret ID"`
}

type DeleteSwarmSecretOutput struct {
	Body base.ApiResponse[base.MessageResponse]
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
func RegisterSwarm(api huma.API, swarmSvc *services.SwarmService, environmentSvc *services.EnvironmentService, eventSvc *services.EventService, cfg *config.Config) {
	h := &SwarmHandler{
		swarmService:       swarmSvc,
		environmentService: environmentSvc,
		eventService:       eventSvc,
		cfg:                cfg,
	}

	huma.Register(api, huma.Operation{OperationID: "list-swarm-services", Method: http.MethodGet, Path: "/environments/{id}/swarm/services", Summary: "List swarm services", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListServices)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-service", Method: http.MethodGet, Path: "/environments/{id}/swarm/services/{serviceId}", Summary: "Get swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetService)
	huma.Register(api, huma.Operation{OperationID: "create-swarm-service", Method: http.MethodPost, Path: "/environments/{id}/swarm/services", Summary: "Create swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.CreateService)
	huma.Register(api, huma.Operation{OperationID: "update-swarm-service", Method: http.MethodPut, Path: "/environments/{id}/swarm/services/{serviceId}", Summary: "Update swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.UpdateService)
	huma.Register(api, huma.Operation{OperationID: "delete-swarm-service", Method: http.MethodDelete, Path: "/environments/{id}/swarm/services/{serviceId}", Summary: "Delete swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DeleteService)
	huma.Register(api, huma.Operation{OperationID: "list-swarm-service-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/services/{serviceId}/tasks", Summary: "List tasks for a swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListServiceTasks)
	huma.Register(api, huma.Operation{OperationID: "rollback-swarm-service", Method: http.MethodPost, Path: "/environments/{id}/swarm/services/{serviceId}/rollback", Summary: "Rollback a swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.RollbackService)
	huma.Register(api, huma.Operation{OperationID: "scale-swarm-service", Method: http.MethodPost, Path: "/environments/{id}/swarm/services/{serviceId}/scale", Summary: "Scale a swarm service", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ScaleService)

	huma.Register(api, huma.Operation{OperationID: "list-swarm-nodes", Method: http.MethodGet, Path: "/environments/{id}/swarm/nodes", Summary: "List swarm nodes", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListNodes)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-node", Method: http.MethodGet, Path: "/environments/{id}/swarm/nodes/{nodeId}", Summary: "Get swarm node", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetNode)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-node-agent-deployment", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/{nodeId}/agent/deployment", Summary: "Get swarm node agent deployment snippets", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetNodeAgentDeployment)
	huma.Register(api, huma.Operation{OperationID: "update-swarm-node", Method: http.MethodPatch, Path: "/environments/{id}/swarm/nodes/{nodeId}", Summary: "Update swarm node", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.UpdateNode)
	huma.Register(api, huma.Operation{OperationID: "delete-swarm-node", Method: http.MethodDelete, Path: "/environments/{id}/swarm/nodes/{nodeId}", Summary: "Delete swarm node", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DeleteNode)
	huma.Register(api, huma.Operation{OperationID: "promote-swarm-node", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/{nodeId}/promote", Summary: "Promote swarm node", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.PromoteNode)
	huma.Register(api, huma.Operation{OperationID: "demote-swarm-node", Method: http.MethodPost, Path: "/environments/{id}/swarm/nodes/{nodeId}/demote", Summary: "Demote swarm node", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DemoteNode)
	huma.Register(api, huma.Operation{OperationID: "list-swarm-node-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/nodes/{nodeId}/tasks", Summary: "List tasks for a swarm node", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListNodeTasks)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-node-identity", Method: http.MethodGet, Path: "/swarm/node-identity", Summary: "Get local swarm node identity", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetNodeIdentity)

	huma.Register(api, huma.Operation{OperationID: "list-swarm-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/tasks", Summary: "List swarm tasks", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListTasks)

	huma.Register(api, huma.Operation{OperationID: "list-swarm-stacks", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks", Summary: "List swarm stacks", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListStacks)
	huma.Register(api, huma.Operation{OperationID: "deploy-swarm-stack", Method: http.MethodPost, Path: "/environments/{id}/swarm/stacks", Summary: "Deploy swarm stack", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DeployStack)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-stack", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}", Summary: "Get swarm stack", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetStack)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-stack-source", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}/source", Summary: "Get swarm stack source", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetStackSource)
	huma.Register(api, huma.Operation{OperationID: "delete-swarm-stack", Method: http.MethodDelete, Path: "/environments/{id}/swarm/stacks/{name}", Summary: "Delete swarm stack", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DeleteStack)
	huma.Register(api, huma.Operation{OperationID: "list-swarm-stack-services", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}/services", Summary: "List swarm stack services", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListStackServices)
	huma.Register(api, huma.Operation{OperationID: "list-swarm-stack-tasks", Method: http.MethodGet, Path: "/environments/{id}/swarm/stacks/{name}/tasks", Summary: "List swarm stack tasks", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListStackTasks)
	huma.Register(api, huma.Operation{OperationID: "render-swarm-stack-config", Method: http.MethodPost, Path: "/environments/{id}/swarm/stacks/config/render", Summary: "Render/validate swarm stack config", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.RenderStackConfig)

	huma.Register(api, huma.Operation{OperationID: "get-swarm-status", Method: http.MethodGet, Path: "/environments/{id}/swarm/status", Summary: "Get swarm status", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetSwarmStatus)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-info", Method: http.MethodGet, Path: "/environments/{id}/swarm/info", Summary: "Get swarm info", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetSwarmInfo)
	huma.Register(api, huma.Operation{OperationID: "init-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/init", Summary: "Initialize swarm", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.InitSwarm)
	huma.Register(api, huma.Operation{OperationID: "join-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/join", Summary: "Join swarm", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.JoinSwarm)
	huma.Register(api, huma.Operation{OperationID: "leave-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/leave", Summary: "Leave swarm", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.LeaveSwarm)
	huma.Register(api, huma.Operation{OperationID: "unlock-swarm", Method: http.MethodPost, Path: "/environments/{id}/swarm/unlock", Summary: "Unlock swarm", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.UnlockSwarm)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-unlock-key", Method: http.MethodGet, Path: "/environments/{id}/swarm/unlock-key", Summary: "Get swarm unlock key", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetUnlockKey)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-join-tokens", Method: http.MethodGet, Path: "/environments/{id}/swarm/join-tokens", Summary: "Get swarm join tokens", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetJoinTokens)
	huma.Register(api, huma.Operation{OperationID: "rotate-swarm-join-tokens", Method: http.MethodPost, Path: "/environments/{id}/swarm/join-tokens/rotate", Summary: "Rotate swarm join tokens", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.RotateJoinTokens)
	huma.Register(api, huma.Operation{OperationID: "update-swarm-spec", Method: http.MethodPut, Path: "/environments/{id}/swarm/spec", Summary: "Update swarm spec", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.UpdateSwarmSpec)

	huma.Register(api, huma.Operation{OperationID: "list-swarm-configs", Method: http.MethodGet, Path: "/environments/{id}/swarm/configs", Summary: "List swarm configs", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListConfigs)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-config", Method: http.MethodGet, Path: "/environments/{id}/swarm/configs/{configId}", Summary: "Get swarm config", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetConfig)
	huma.Register(api, huma.Operation{OperationID: "create-swarm-config", Method: http.MethodPost, Path: "/environments/{id}/swarm/configs", Summary: "Create swarm config", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.CreateConfig)
	huma.Register(api, huma.Operation{OperationID: "update-swarm-config", Method: http.MethodPut, Path: "/environments/{id}/swarm/configs/{configId}", Summary: "Update swarm config", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.UpdateConfig)
	huma.Register(api, huma.Operation{OperationID: "delete-swarm-config", Method: http.MethodDelete, Path: "/environments/{id}/swarm/configs/{configId}", Summary: "Delete swarm config", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DeleteConfig)

	huma.Register(api, huma.Operation{OperationID: "list-swarm-secrets", Method: http.MethodGet, Path: "/environments/{id}/swarm/secrets", Summary: "List swarm secrets", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.ListSecrets)
	huma.Register(api, huma.Operation{OperationID: "get-swarm-secret", Method: http.MethodGet, Path: "/environments/{id}/swarm/secrets/{secretId}", Summary: "Get swarm secret", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.GetSecret)
	huma.Register(api, huma.Operation{OperationID: "create-swarm-secret", Method: http.MethodPost, Path: "/environments/{id}/swarm/secrets", Summary: "Create swarm secret", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.CreateSecret)
	huma.Register(api, huma.Operation{OperationID: "update-swarm-secret", Method: http.MethodPut, Path: "/environments/{id}/swarm/secrets/{secretId}", Summary: "Update swarm secret", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.UpdateSecret)
	huma.Register(api, huma.Operation{OperationID: "delete-swarm-secret", Method: http.MethodDelete, Path: "/environments/{id}/swarm/secrets/{secretId}", Summary: "Delete swarm secret", Tags: []string{"Swarm"}, Security: []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}}, h.DeleteSecret)
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
func (h *SwarmHandler) ListServices(ctx context.Context, input *ListSwarmServicesInput) (*ListSwarmServicesOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
	items, paginationResp, err := h.swarmService.ListServicesPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceListError{Err: err}).Error())
	}
	if items == nil {
		items = []swarmtypes.ServiceSummary{}
	}

	return &ListSwarmServicesOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) GetService(ctx context.Context, input *GetSwarmServiceInput) (*GetSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	service, err := h.swarmService.GetService(ctx, input.ServiceID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmServiceNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceNotFoundError{Err: err}).Error())
	}

	return &GetSwarmServiceOutput{Body: base.ApiResponse[swarmtypes.ServiceInspect]{Success: true, Data: *service}}, nil
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
func (h *SwarmHandler) CreateService(ctx context.Context, input *CreateSwarmServiceInput) (*CreateSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.CreateService(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceCreateError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.create", "swarm_service", resp.ID, "", map[string]any{"serviceId": resp.ID})

	return &CreateSwarmServiceOutput{Body: base.ApiResponse[swarmtypes.ServiceCreateResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) UpdateService(ctx context.Context, input *UpdateSwarmServiceInput) (*UpdateSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.UpdateService(ctx, input.ServiceID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceUpdateError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.update", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &UpdateSwarmServiceOutput{Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) DeleteService(ctx context.Context, input *DeleteSwarmServiceInput) (*DeleteSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.RemoveService(ctx, input.ServiceID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmServiceNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceRemoveError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.delete", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &DeleteSwarmServiceOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm service removed successfully"}}}, nil
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
func (h *SwarmHandler) ListServiceTasks(ctx context.Context, input *ListSwarmServiceTasksInput) (*ListSwarmServiceTasksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
	items, paginationResp, err := h.swarmService.ListServiceTasksPaginated(ctx, input.ServiceID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmTaskListError{Err: err}).Error())
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &ListSwarmServiceTasksOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) RollbackService(ctx context.Context, input *RollbackSwarmServiceInput) (*RollbackSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.RollbackService(ctx, input.ServiceID)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceUpdateError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.rollback", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID})

	return &RollbackSwarmServiceOutput{Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) ScaleService(ctx context.Context, input *ScaleSwarmServiceInput) (*ScaleSwarmServiceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.ScaleService(ctx, input.ServiceID, input.Body.Replicas)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmServiceUpdateError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "service.scale", "swarm_service", input.ServiceID, "", map[string]any{"serviceId": input.ServiceID, "replicas": input.Body.Replicas})

	return &ScaleSwarmServiceOutput{Body: base.ApiResponse[swarmtypes.ServiceUpdateResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) ListNodes(ctx context.Context, input *ListSwarmNodesInput) (*ListSwarmNodesOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
	items, paginationResp, err := h.swarmService.ListNodesPaginated(ctx, input.EnvironmentID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeListError{Err: err}).Error())
	}
	if items == nil {
		items = []swarmtypes.NodeSummary{}
	}

	return &ListSwarmNodesOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) GetNode(ctx context.Context, input *GetSwarmNodeInput) (*GetSwarmNodeOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	node, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmNodeNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	return &GetSwarmNodeOutput{Body: base.ApiResponse[swarmtypes.NodeSummary]{Success: true, Data: *node}}, nil
}

// GetNodeAgentDeployment returns deployment snippets for attaching an Arcane agent to a swarm node.
//
// It requires admin privileges, ensures a hidden node-agent environment exists
// for the target node, optionally rotates the environment token, generates edge
// deployment snippets, and refreshes the node summary so the response includes
// the latest agent status.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the environment and node and optionally requests token rotation.
//
// Returns deployment snippets, the backing environment ID, and the refreshed agent status.
// Returns an authorization error for non-admin callers, `401 Unauthorized`
// when the current user cannot be resolved, `404 Not Found` when the node does
// not exist, or `500 Internal Server Error` when environment provisioning or
// snippet generation fails.
func (h *SwarmHandler) GetNodeAgentDeployment(ctx context.Context, input *GetSwarmNodeAgentDeploymentInput) (*GetSwarmNodeAgentDeploymentOutput, error) {
	if h.swarmService == nil || h.environmentService == nil || h.cfg == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	node, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound((&common.SwarmNodeNotFoundError{Err: err}).Error())
		}
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	user, ok := humamw.GetCurrentUserFromContext(ctx)
	if !ok || user == nil {
		return nil, huma.Error401Unauthorized("Unauthorized")
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

	snippets, err := h.environmentService.GenerateEdgeDeploymentSnippets(ctx, env.ID, h.cfg.GetAppURL(), apiKey)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	updatedNode, err := h.swarmService.GetNode(ctx, input.EnvironmentID, input.NodeID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &GetSwarmNodeAgentDeploymentOutput{
		Body: base.ApiResponse[SwarmNodeAgentDeployment]{
			Success: true,
			Data: SwarmNodeAgentDeployment{
				DeploymentSnippet: DeploymentSnippet{
					DockerRun:     snippets.DockerRun,
					DockerCompose: snippets.DockerCompose,
				},
				EnvironmentID: env.ID,
				Agent:         updatedNode.Agent,
			},
		},
	}, nil
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
func (h *SwarmHandler) GetNodeIdentity(ctx context.Context, _ *GetSwarmNodeIdentityInput) (*GetSwarmNodeIdentityOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	identity, err := h.swarmService.GetLocalNodeIdentity(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}

	return &GetSwarmNodeIdentityOutput{
		Body: base.ApiResponse[services.SwarmNodeIdentity]{
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
func (h *SwarmHandler) UpdateNode(ctx context.Context, input *UpdateSwarmNodeInput) (*UpdateSwarmNodeOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.UpdateNode(ctx, input.NodeID, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.update", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return &UpdateSwarmNodeOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node updated successfully"}}}, nil
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
func (h *SwarmHandler) DeleteNode(ctx context.Context, input *DeleteSwarmNodeInput) (*DeleteSwarmNodeOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.RemoveNode(ctx, input.NodeID, input.Force); err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.delete", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID, "force": input.Force})

	return &DeleteSwarmNodeOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node removed successfully"}}}, nil
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
func (h *SwarmHandler) PromoteNode(ctx context.Context, input *PromoteSwarmNodeInput) (*PromoteSwarmNodeOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.PromoteNode(ctx, input.NodeID); err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.promote", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return &PromoteSwarmNodeOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node promoted successfully"}}}, nil
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
func (h *SwarmHandler) DemoteNode(ctx context.Context, input *DemoteSwarmNodeInput) (*DemoteSwarmNodeOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.DemoteNode(ctx, input.NodeID); err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmNodeNotFoundError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "node.demote", "swarm_node", input.NodeID, "", map[string]any{"nodeId": input.NodeID})

	return &DemoteSwarmNodeOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm node demoted successfully"}}}, nil
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
func (h *SwarmHandler) ListNodeTasks(ctx context.Context, input *ListSwarmNodeTasksInput) (*ListSwarmNodeTasksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
	items, paginationResp, err := h.swarmService.ListNodeTasksPaginated(ctx, input.NodeID, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmTaskListError{Err: err}).Error())
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &ListSwarmNodeTasksOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) ListTasks(ctx context.Context, input *ListSwarmTasksInput) (*ListSwarmTasksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
	items, paginationResp, err := h.swarmService.ListTasksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmTaskListError{Err: err}).Error())
	}
	if items == nil {
		items = []swarmtypes.TaskSummary{}
	}

	return &ListSwarmTasksOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) ListStacks(ctx context.Context, input *ListSwarmStacksInput) (*ListSwarmStacksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
	items, paginationResp, err := h.swarmService.ListStacksPaginated(ctx, params)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmStackListError{Err: err}).Error())
	}
	if items == nil {
		items = []swarmtypes.StackSummary{}
	}

	return &ListSwarmStacksOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) DeployStack(ctx context.Context, input *DeploySwarmStackInput) (*DeploySwarmStackOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.DeployStack(ctx, input.EnvironmentID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmStackDeployError{Err: err}).Error())
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "stack.deploy", "swarm_stack", input.Body.Name, input.Body.Name, map[string]any{"stack": input.Body.Name})

	return &DeploySwarmStackOutput{Body: base.ApiResponse[swarmtypes.StackDeployResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) GetStack(ctx context.Context, input *GetSwarmStackInput) (*GetSwarmStackOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	stack, err := h.swarmService.GetStack(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound(("Swarm stack not found"))
		}
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm stack")
	}

	return &GetSwarmStackOutput{Body: base.ApiResponse[swarmtypes.StackInspect]{Success: true, Data: *stack}}, nil
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
func (h *SwarmHandler) GetStackSource(ctx context.Context, input *GetSwarmStackSourceInput) (*GetSwarmStackSourceOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	source, err := h.swarmService.GetStackSource(ctx, input.EnvironmentID, input.Name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack source not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to load swarm stack source")
	}

	return &GetSwarmStackSourceOutput{Body: base.ApiResponse[swarmtypes.StackSource]{Success: true, Data: *source}}, nil
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
func (h *SwarmHandler) DeleteStack(ctx context.Context, input *DeleteSwarmStackInput) (*DeleteSwarmStackOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.RemoveStack(ctx, input.EnvironmentID, input.Name); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm stack not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to remove swarm stack")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "stack.delete", "swarm_stack", input.Name, input.Name, map[string]any{"stack": input.Name})

	return &DeleteSwarmStackOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm stack removed successfully"}}}, nil
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
func (h *SwarmHandler) ListStackServices(ctx context.Context, input *ListSwarmStackServicesInput) (*ListSwarmStackServicesOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
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

	return &ListSwarmStackServicesOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) ListStackTasks(ctx context.Context, input *ListSwarmStackTasksInput) (*ListSwarmStackTasksOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	params := buildSwarmQueryParams(input.Search, input.Sort, input.Order, input.Start, input.Limit)
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

	return &ListSwarmStackTasksOutput{Body: toSwarmPaginatedResponse(items, paginationResp)}, nil
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
func (h *SwarmHandler) RenderStackConfig(ctx context.Context, input *RenderSwarmStackConfigInput) (*RenderSwarmStackConfigOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	resp, err := h.swarmService.RenderStackConfig(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to render swarm stack config")
	}

	return &RenderSwarmStackConfigOutput{Body: base.ApiResponse[swarmtypes.StackRenderConfigResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) GetSwarmStatus(ctx context.Context, input *GetSwarmStatusInput) (*GetSwarmStatusOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	enabled, err := h.swarmService.IsEnabled(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to read swarm status")
	}

	return &GetSwarmStatusOutput{
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
func (h *SwarmHandler) GetSwarmInfo(ctx context.Context, input *GetSwarmInfoInput) (*GetSwarmInfoOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	info, err := h.swarmService.GetSwarmInfo(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, (&common.SwarmInspectError{Err: err}).Error())
	}

	return &GetSwarmInfoOutput{Body: base.ApiResponse[swarmtypes.SwarmInfo]{Success: true, Data: *info}}, nil
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
func (h *SwarmHandler) InitSwarm(ctx context.Context, input *InitSwarmInput) (*InitSwarmOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.InitSwarm(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to initialize swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.init", "swarm", "cluster", "cluster", map[string]any{"nodeId": resp.NodeID})

	return &InitSwarmOutput{Body: base.ApiResponse[swarmtypes.SwarmInitResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) JoinSwarm(ctx context.Context, input *JoinSwarmInput) (*JoinSwarmOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.JoinSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to join swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.join", "swarm", "cluster", "cluster", map[string]any{"remoteAddrs": input.Body.RemoteAddrs})

	return &JoinSwarmOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Joined swarm successfully"}}}, nil
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
func (h *SwarmHandler) LeaveSwarm(ctx context.Context, input *LeaveSwarmInput) (*LeaveSwarmOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.LeaveSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to leave swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.leave", "swarm", "cluster", "cluster", map[string]any{"force": input.Body.Force})

	return &LeaveSwarmOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Left swarm successfully"}}}, nil
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
func (h *SwarmHandler) UnlockSwarm(ctx context.Context, input *UnlockSwarmInput) (*UnlockSwarmOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.UnlockSwarm(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to unlock swarm")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.unlock", "swarm", "cluster", "cluster", map[string]any{})

	return &UnlockSwarmOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm unlocked successfully"}}}, nil
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
func (h *SwarmHandler) GetUnlockKey(ctx context.Context, input *GetSwarmUnlockKeyInput) (*GetSwarmUnlockKeyOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.GetSwarmUnlockKey(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to get swarm unlock key")
	}

	return &GetSwarmUnlockKeyOutput{Body: base.ApiResponse[swarmtypes.SwarmUnlockKeyResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) GetJoinTokens(ctx context.Context, input *GetSwarmJoinTokensInput) (*GetSwarmJoinTokensOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	resp, err := h.swarmService.GetSwarmJoinTokens(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to get swarm join tokens")
	}

	return &GetSwarmJoinTokensOutput{Body: base.ApiResponse[swarmtypes.SwarmJoinTokensResponse]{Success: true, Data: *resp}}, nil
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
func (h *SwarmHandler) RotateJoinTokens(ctx context.Context, input *RotateSwarmJoinTokensInput) (*RotateSwarmJoinTokensOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.RotateSwarmJoinTokens(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to rotate swarm join tokens")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.rotate_tokens", "swarm", "cluster", "cluster", map[string]any{"rotateWorker": input.Body.RotateWorkerToken, "rotateManager": input.Body.RotateManagerToken})

	return &RotateSwarmJoinTokensOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm join tokens rotated successfully"}}}, nil
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
func (h *SwarmHandler) UpdateSwarmSpec(ctx context.Context, input *UpdateSwarmSpecInput) (*UpdateSwarmSpecOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.UpdateSwarmSpec(ctx, input.Body); err != nil {
		return nil, mapSwarmServiceError(err, "Failed to update swarm spec")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "lifecycle.update_spec", "swarm", "cluster", "cluster", map[string]any{})

	return &UpdateSwarmSpecOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm spec updated successfully"}}}, nil
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
func (h *SwarmHandler) ListConfigs(ctx context.Context, input *ListSwarmConfigsInput) (*ListSwarmConfigsOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	items, err := h.swarmService.ListConfigs(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to list swarm configs")
	}
	if items == nil {
		items = []swarmtypes.ConfigSummary{}
	}

	return &ListSwarmConfigsOutput{Body: base.ApiResponse[[]swarmtypes.ConfigSummary]{Success: true, Data: items}}, nil
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
func (h *SwarmHandler) GetConfig(ctx context.Context, input *GetSwarmConfigInput) (*GetSwarmConfigOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	cfg, err := h.swarmService.GetConfig(ctx, input.ConfigID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm config not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm config")
	}

	return &GetSwarmConfigOutput{Body: base.ApiResponse[swarmtypes.ConfigSummary]{Success: true, Data: *cfg}}, nil
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
func (h *SwarmHandler) CreateConfig(ctx context.Context, input *CreateSwarmConfigInput) (*CreateSwarmConfigOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	cfg, err := h.swarmService.CreateConfig(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to create swarm config")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "config.create", "swarm_config", cfg.ID, cfg.Spec.Name, map[string]any{"configId": cfg.ID, "name": cfg.Spec.Name})

	return &CreateSwarmConfigOutput{Body: base.ApiResponse[swarmtypes.ConfigSummary]{Success: true, Data: *cfg}}, nil
}

// UpdateConfig updates an existing swarm config.
//
// It requires admin privileges, delegates the update to the swarm service, and
// records an audit event containing the config ID and updated name.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the config to update and contains the replacement specification.
//
// Returns the updated config summary.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the update fails.
func (h *SwarmHandler) UpdateConfig(ctx context.Context, input *UpdateSwarmConfigInput) (*UpdateSwarmConfigOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	cfg, err := h.swarmService.UpdateConfig(ctx, input.ConfigID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to update swarm config")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "config.update", "swarm_config", input.ConfigID, cfg.Spec.Name, map[string]any{"configId": input.ConfigID, "name": cfg.Spec.Name})

	return &UpdateSwarmConfigOutput{Body: base.ApiResponse[swarmtypes.ConfigSummary]{Success: true, Data: *cfg}}, nil
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
func (h *SwarmHandler) DeleteConfig(ctx context.Context, input *DeleteSwarmConfigInput) (*DeleteSwarmConfigOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.RemoveConfig(ctx, input.ConfigID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm config not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to remove swarm config")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "config.delete", "swarm_config", input.ConfigID, "", map[string]any{"configId": input.ConfigID})

	return &DeleteSwarmConfigOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm config removed successfully"}}}, nil
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
func (h *SwarmHandler) ListSecrets(ctx context.Context, input *ListSwarmSecretsInput) (*ListSwarmSecretsOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	items, err := h.swarmService.ListSecrets(ctx)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to list swarm secrets")
	}
	if items == nil {
		items = []swarmtypes.SecretSummary{}
	}

	return &ListSwarmSecretsOutput{Body: base.ApiResponse[[]swarmtypes.SecretSummary]{Success: true, Data: items}}, nil
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
func (h *SwarmHandler) GetSecret(ctx context.Context, input *GetSwarmSecretInput) (*GetSwarmSecretOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}

	secret, err := h.swarmService.GetSecret(ctx, input.SecretID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm secret not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to inspect swarm secret")
	}

	return &GetSwarmSecretOutput{Body: base.ApiResponse[swarmtypes.SecretSummary]{Success: true, Data: *secret}}, nil
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
func (h *SwarmHandler) CreateSecret(ctx context.Context, input *CreateSwarmSecretInput) (*CreateSwarmSecretOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	secret, err := h.swarmService.CreateSecret(ctx, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to create swarm secret")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "secret.create", "swarm_secret", secret.ID, secret.Spec.Name, map[string]any{"secretId": secret.ID, "name": secret.Spec.Name})

	return &CreateSwarmSecretOutput{Body: base.ApiResponse[swarmtypes.SecretSummary]{Success: true, Data: *secret}}, nil
}

// UpdateSecret updates an existing swarm secret.
//
// It requires admin privileges, delegates the update to the swarm service, and
// records an audit event containing the secret ID and updated name.
//
// ctx carries request-scoped cancellation, auth, and audit context.
// input identifies the secret to update and contains the replacement specification.
//
// Returns the updated secret summary.
// Returns an authorization error for non-admin callers or mapped HTTP errors
// when the update fails.
func (h *SwarmHandler) UpdateSecret(ctx context.Context, input *UpdateSwarmSecretInput) (*UpdateSwarmSecretOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	secret, err := h.swarmService.UpdateSecret(ctx, input.SecretID, input.Body)
	if err != nil {
		return nil, mapSwarmServiceError(err, "Failed to update swarm secret")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "secret.update", "swarm_secret", input.SecretID, secret.Spec.Name, map[string]any{"secretId": input.SecretID, "name": secret.Spec.Name})

	return &UpdateSwarmSecretOutput{Body: base.ApiResponse[swarmtypes.SecretSummary]{Success: true, Data: *secret}}, nil
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
func (h *SwarmHandler) DeleteSecret(ctx context.Context, input *DeleteSwarmSecretInput) (*DeleteSwarmSecretOutput, error) {
	if h.swarmService == nil {
		return nil, huma.Error500InternalServerError("service not available")
	}
	if err := checkAdmin(ctx); err != nil {
		return nil, err
	}

	if err := h.swarmService.RemoveSecret(ctx, input.SecretID); err != nil {
		if errdefs.IsNotFound(err) {
			return nil, huma.Error404NotFound("Swarm secret not found")
		}
		return nil, mapSwarmServiceError(err, "Failed to remove swarm secret")
	}

	h.auditSwarmMutation(ctx, input.EnvironmentID, "secret.delete", "swarm_secret", input.SecretID, "", map[string]any{"secretId": input.SecretID})

	return &DeleteSwarmSecretOutput{Body: base.ApiResponse[base.MessageResponse]{Success: true, Data: base.MessageResponse{Message: "Swarm secret removed successfully"}}}, nil
}

// toSwarmPaginatedResponse wraps items and pagination metadata in the standard swarm list envelope.
//
// items is the collection to include in the response body.
// p provides the pagination metadata produced by the pagination package.
//
// Returns a SwarmPaginatedResponse with `Success` set to true.
func toSwarmPaginatedResponse[T any](items []T, p pagination.Response) SwarmPaginatedResponse[T] {
	return SwarmPaginatedResponse[T]{
		Success: true,
		Data:    items,
		Pagination: base.PaginationResponse{
			TotalPages:      p.TotalPages,
			TotalItems:      p.TotalItems,
			CurrentPage:     p.CurrentPage,
			ItemsPerPage:    p.ItemsPerPage,
			GrandTotalItems: p.GrandTotalItems,
		},
	}
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
	if user, ok := humamw.GetCurrentUserFromContext(ctx); ok {
		uid := user.ID
		uname := user.Username
		userID = &uid
		username = &uname
	}

	var resourceTypePtr *string
	if strings.TrimSpace(resourceType) != "" {
		rt := resourceType
		resourceTypePtr = &rt
	}
	var resourceIDPtr *string
	if strings.TrimSpace(resourceID) != "" {
		rid := resourceID
		resourceIDPtr = &rid
	}
	var resourceNamePtr *string
	if strings.TrimSpace(resourceName) != "" {
		rn := resourceName
		resourceNamePtr = &rn
	}

	env := strings.TrimSpace(environmentID)
	if env == "" {
		env = "0"
	}
	envPtr := &env

	meta := models.JSON{"action": action}
	maps.Copy(meta, metadata)

	_, err := h.eventService.CreateEvent(ctx, services.CreateEventRequest{
		Type:          models.EventType("swarm." + action),
		Severity:      models.EventSeverityInfo,
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

// buildSwarmQueryParams converts raw request values into pagination query parameters.
//
// It trims string inputs, applies the default limit used by the swarm API, and
// preserves the requested start offset.
//
// search is the free-text search term.
// sort is the requested sort column.
// order is the requested sort direction.
// start is the zero-based pagination offset.
// limit is the requested page size.
//
// Returns normalized pagination.QueryParams for downstream service calls.
func buildSwarmQueryParams(search, sort, order string, start, limit int) pagination.QueryParams {
	if limit == 0 {
		limit = 20
	}

	return pagination.QueryParams{
		SearchQuery: pagination.SearchQuery{
			Search: strings.TrimSpace(search),
		},
		SortParams: pagination.SortParams{
			Sort:  strings.TrimSpace(sort),
			Order: pagination.SortOrder(order),
		},
		PaginationParams: pagination.PaginationParams{
			Start: start,
			Limit: limit,
		},
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
	if errors.Is(err, services.ErrSwarmNotEnabled) {
		return huma.Error409Conflict((&common.SwarmNotEnabledError{}).Error())
	}
	if errors.Is(err, services.ErrSwarmManagerRequired) {
		return huma.Error403Forbidden((&common.SwarmManagerRequiredError{}).Error())
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
	if strings.Contains(errText, "required") || strings.Contains(errText, "invalid") || strings.Contains(errText, "immutable") {
		return huma.Error400BadRequest(err.Error())
	}
	return huma.Error500InternalServerError(fallback)
}
