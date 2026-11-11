package ws

import (
	"net/http"
	"slices"
	"strings"

	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
)

func (h *WebSocketHandler) proxiedRoutes() []middleware.EchoRoute {
	return []middleware.EchoRoute{
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/projects/:projectId/logs",
			Permission: authz.PermProjectsLogs,
			Handler:    h.ProjectLogs,
		},
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/containers/stats",
			Permission: authz.PermContainersRead,
			Handler:    h.ContainersStats,
		},
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/containers/:containerId/logs",
			Permission: authz.PermContainersLogs,
			Handler:    h.ContainerLogs,
		},
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/containers/:containerId/stats",
			Permission: authz.PermContainersRead,
			Handler:    h.ContainerStats,
		},
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/containers/:containerId/terminal",
			Permission: authz.PermContainersExec,
			Handler:    h.ContainerExec,
		},
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/swarm/services/:serviceId/logs",
			Permission: authz.PermSwarmServicesLogs,
			Handler:    h.ServiceLogs,
		},
		{
			Method:     http.MethodGet,
			Path:       "/agents/:id/ws/system/stats",
			Permission: authz.PermSystemRead,
			Handler:    h.SystemStats,
		},
	}
}

func AddProxiedPermissions(m *authz.PermissionMatcher) {
	var h WebSocketHandler
	for _, r := range h.proxiedRoutes() {
		m.Add(r.Method, strings.TrimPrefix(r.Path, "/agents/:id"), r.Permission)
	}
}

func Routes() []middleware.EchoRoute {
	var h WebSocketHandler
	return slices.Concat(h.proxiedRoutes(), h.diagnosticsRoutes())
}
