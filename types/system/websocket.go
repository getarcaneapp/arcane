package system

import "time"

// WebSocket connection kind constants.
const (
	WSKindProjectLogs    = "project_logs"
	WSKindContainerLogs  = "container_logs"
	WSKindContainerStats = "container_stats"
	WSKindContainerExec  = "container_exec"
	WSKindSystemStats    = "system_stats"
	WSKindServiceLogs    = "service_logs"
)

// WebSocketConnectionInfo describes a single active WebSocket connection.
type WebSocketConnectionInfo struct {
	// ID is the unique identifier for the connection.
	//
	// Required: true
	ID string `json:"id"`
	// Kind is the type of WebSocket connection (e.g. project_logs, container_stats).
	//
	// Required: true
	Kind string `json:"kind"`
	// EnvID is the environment the connection belongs to.
	EnvID string `json:"envId,omitempty"`
	// ResourceID is the ID of the resource being streamed (container, project, etc.).
	ResourceID string `json:"resourceId,omitempty"`
	// ClientIP is the remote address of the client.
	ClientIP string `json:"clientIp,omitempty"`
	// UserID is the authenticated user who opened the connection.
	UserID string `json:"userId,omitempty"`
	// UserAgent is the HTTP User-Agent header from the client.
	UserAgent string `json:"userAgent,omitempty"`
	// StartedAt is when the connection was established.
	//
	// Required: true
	StartedAt time.Time `json:"startedAt"`
}

// WebSocketMetricsSnapshot is a point-in-time copy of active WebSocket
// connection counts, broken down by kind.
type WebSocketMetricsSnapshot struct {
	// ProjectLogsActive is the number of active project-log streams.
	ProjectLogsActive int64 `json:"projectLogsActive"`
	// ContainerLogsActive is the number of active container-log streams.
	ContainerLogsActive int64 `json:"containerLogsActive"`
	// ContainerStats is the number of active container-stats streams.
	ContainerStats int64 `json:"containerStats"`
	// ContainerExec is the number of active container-exec sessions.
	ContainerExec int64 `json:"containerExec"`
	// SystemStats is the number of active system-stats streams.
	SystemStats int64 `json:"systemStats"`
	// ServiceLogsActive is the number of active swarm service-log streams.
	ServiceLogsActive int64 `json:"serviceLogsActive"`
}
