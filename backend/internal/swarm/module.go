// Package swarm owns Docker Swarm management and its HTTP routes.
package swarm

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/configs"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/nodes"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/secrets"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/services"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm/children/stacks"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

type Module struct {
	service     *SwarmService
	environment *environment.EnvironmentService
	event       *event.EventService
	config      *config.Config
}

func New(service *SwarmService, environmentService *environment.EnvironmentService, eventService *event.EventService, cfg *config.Config) *Module {
	return &Module{service: service, environment: environmentService, event: eventService, config: cfg}
}

func (m *Module) Service() *SwarmService {
	if m == nil {
		return nil
	}
	return m.service
}

func (m *Module) RegisterRoutes(api huma.API) {
	if m == nil {
		m = &Module{}
	}
	service := m.service
	if service == nil {
		service = &SwarmService{}
	}

	h := NewHandler(m.service, m.event)
	RegisterSwarm(api, h)
	services.RegisterRoutes(api, services.NewHandler(service.services, h.auditSwarmMutation, mapSwarmServiceError))
	nodes.RegisterRoutes(api, nodes.NewHandler(service.nodes, h.auditSwarmMutation, mapSwarmServiceError, m.environment, m.config))
	stacks.RegisterRoutes(api, stacks.NewHandler(service.stacks, h.auditSwarmMutation, mapSwarmServiceError))
	configs.RegisterRoutes(api, configs.NewHandler(service.configs, h.auditSwarmMutation, mapSwarmServiceError))
	secrets.RegisterRoutes(api, secrets.NewHandler(service.secrets, h.auditSwarmMutation, mapSwarmServiceError))
}

// RegisterSwarm registers the swarm task and cluster lifecycle operations.
func RegisterSwarm(api huma.API, h *SwarmHandler) {
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "list-swarm-tasks",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/tasks",
			Summary:     "List swarm tasks",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.ListTasks,
	)

	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-status",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/status",
			Summary:     "Get swarm status",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetSwarmStatus,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-info",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/info",
			Summary:     "Get swarm info",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmRead,
		h.GetSwarmInfo,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "init-swarm",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/init",
			Summary:     "Initialize swarm",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmInit,
		h.InitSwarm,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "join-swarm",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/join",
			Summary:     "Join swarm",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmJoin,
		h.JoinSwarm,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "leave-swarm",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/leave",
			Summary:     "Leave swarm",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmLeave,
		h.LeaveSwarm,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "unlock-swarm",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/unlock",
			Summary:     "Unlock swarm",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmUnlock,
		h.UnlockSwarm,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-unlock-key",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/unlock/key",
			Summary:     "Get swarm unlock key",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmUnlock,
		h.GetUnlockKey,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "get-swarm-join-tokens",
			Method:      http.MethodGet,
			Path:        "/environments/{id}/swarm/join/tokens",
			Summary:     "Get swarm join tokens",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmUnlock,
		h.GetJoinTokens,
	)
	middleware.RegisterWithActionPermissions(
		api,
		huma.Operation{
			OperationID: "swarm-join-tokens-action",
			Method:      http.MethodPost,
			Path:        "/environments/{id}/swarm/join/tokens",
			Summary:     "Rotate swarm join tokens",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		"action",
		map[string]string{
			"rotate": authz.PermSwarmUnlock,
		},
		h.RotateJoinTokens,
	)
	middleware.RegisterWithPermission(
		api,
		huma.Operation{
			OperationID: "update-swarm-spec",
			Method:      http.MethodPut,
			Path:        "/environments/{id}/swarm/spec",
			Summary:     "Update swarm spec",
			Tags: []string{
				"Swarm",
			},
			Security: handlerutil.DefaultOperationSecurity(),
		},
		authz.PermSwarmSpec,
		h.UpdateSwarmSpec,
	)
}
