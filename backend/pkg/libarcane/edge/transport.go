package edge

import (
	"net/url"
	"strings"
	"time"
)

// Config contains the public edge-tunnel runtime settings needed by pkg/libarcane/edge.
type Config struct {
	EdgeAgent             bool
	EdgeTransport         string
	EdgeReconnectInterval int
	ManagerApiUrl         string
	AgentToken            string
	Port                  string
	Listen                string
}

// GetManagerBaseURL returns the base URL of the manager application.
// It strips any trailing slashes or /api suffix from MANAGER_API_URL.
func (c *Config) GetManagerBaseURL() string {
	if c == nil || c.ManagerApiUrl == "" {
		return ""
	}
	managerURL := strings.TrimRight(c.ManagerApiUrl, "/")
	managerURL = strings.TrimSuffix(managerURL, "/api")
	return managerURL
}

// GetManagerGRPCAddr returns the manager gRPC address in host:port form.
func (c *Config) GetManagerGRPCAddr() string {
	baseURL := c.GetManagerBaseURL()
	if baseURL == "" {
		return ""
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}

	host := parsed.Hostname()
	if host == "" {
		return ""
	}

	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}

	return host + ":" + port
}

// TunnelRuntimeState describes the live, in-memory state of an active edge tunnel.
type TunnelRuntimeState struct {
	Transport     string
	ConnectedAt   *time.Time
	LastHeartbeat *time.Time
}

const (
	// EdgeTransportWebSocket forces WebSocket tunnel transport.
	EdgeTransportWebSocket = "websocket"
	// EdgeTransportGRPC forces gRPC transport without WebSocket fallback.
	EdgeTransportGRPC = "grpc"
	// EdgeTransportPoll uses an HTTP polling control plane with the existing
	// websocket tunnel as an on-demand data plane.
	EdgeTransportPoll = "poll"
	// EdgeTransportAuto preserves the legacy managed tunnel behavior: try gRPC
	// first and fall back to websocket when available.
	EdgeTransportAuto = "auto"
)

// NormalizeEdgeTransport normalizes transport config and defaults to the legacy
// managed tunnel auto mode for backwards compatibility.
func NormalizeEdgeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EdgeTransportWebSocket:
		return EdgeTransportWebSocket
	case EdgeTransportGRPC:
		return EdgeTransportGRPC
	case EdgeTransportPoll:
		return EdgeTransportPoll
	case EdgeTransportAuto:
		return EdgeTransportAuto
	default:
		return EdgeTransportAuto
	}
}

// UseGRPCEdgeTransport reports whether gRPC managed tunnel mode should be attempted.
func UseGRPCEdgeTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeEdgeTransport(cfg.EdgeTransport)
	return transport == EdgeTransportGRPC || transport == EdgeTransportAuto
}

// UseWebSocketEdgeTransport reports whether websocket managed tunnel mode is allowed.
func UseWebSocketEdgeTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeEdgeTransport(cfg.EdgeTransport)
	return transport == EdgeTransportWebSocket || transport == EdgeTransportAuto
}

// UsePollEdgeTransport reports whether the Portainer-style polling control plane
// should be used.
func UsePollEdgeTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return NormalizeEdgeTransport(cfg.EdgeTransport) == EdgeTransportPoll
}

// GetActiveTunnelTransport returns the currently active tunnel transport for an environment.
func GetActiveTunnelTransport(envID string) (string, bool) {
	tunnel, ok := GetRegistry().Get(envID)
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return "", false
	}

	switch tunnel.Conn.(type) {
	case *GRPCManagerTunnelConn, *GRPCAgentTunnelConn:
		return EdgeTransportGRPC, true
	case *TunnelConn:
		return EdgeTransportWebSocket, true
	default:
		return "", false
	}
}

// GetTunnelRuntimeState returns live metadata for an active tunnel.
func GetTunnelRuntimeState(envID string) (*TunnelRuntimeState, bool) {
	tunnel, ok := GetRegistry().Get(envID)
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return nil, false
	}

	state := &TunnelRuntimeState{}

	switch tunnel.Conn.(type) {
	case *GRPCManagerTunnelConn, *GRPCAgentTunnelConn:
		state.Transport = EdgeTransportGRPC
	case *TunnelConn:
		state.Transport = EdgeTransportWebSocket
	}

	connectedAt := tunnel.ConnectedAt
	lastHeartbeat := tunnel.GetLastHeartbeat()
	state.ConnectedAt = &connectedAt
	state.LastHeartbeat = &lastHeartbeat

	return state, true
}
