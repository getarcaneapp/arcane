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
	EdgeMTLSMode          string
	EdgeMTLSCAFile        string
	EdgeMTLSCertFile      string
	EdgeMTLSKeyFile       string
	EdgeMTLSServerName    string
	EdgeMTLSAssetsDir     string
	AppURL                string
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
	SessionID     string
	AgentInstance string
	SecurityMode  string
	Capabilities  []string
	State         string
}

const (
	// TransportAuto prefers gRPC and falls back to WebSocket automatically.
	TransportAuto = "auto"
	// TransportWebSocket forces WebSocket tunnel transport.
	TransportWebSocket = "websocket"
	// TransportGRPC forces gRPC transport without WebSocket fallback.
	TransportGRPC = "grpc"
	// TransportPoll uses an HTTP polling control plane with the existing
	// websocket tunnel as an on-demand data plane.
	TransportPoll = "poll"

	// MTLSModeDisabled disables edge tunnel mTLS.
	MTLSModeDisabled = "disabled"
	// MTLSModeOptional enables edge tunnel mTLS when certificates are configured.
	MTLSModeOptional = "optional"
	// MTLSModeRequired requires a verified client certificate on edge tunnel endpoints
	// when Arcane terminates TLS; external TLS terminators must enforce mTLS before proxying.
	MTLSModeRequired = "required"
)

// NormalizeEdgeTransport normalizes transport config and defaults to auto-negotiation.
func NormalizeEdgeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TransportAuto:
		return TransportAuto
	case TransportWebSocket:
		return TransportWebSocket
	case TransportGRPC:
		return TransportGRPC
	case TransportPoll:
		return TransportPoll
	default:
		return TransportAuto
	}
}

// NormalizeEdgeMTLSMode normalizes edge mTLS config and defaults to disabled.
func NormalizeEdgeMTLSMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MTLSModeOptional:
		return MTLSModeOptional
	case MTLSModeRequired:
		return MTLSModeRequired
	default:
		return MTLSModeDisabled
	}
}

// UseGRPCEdgeTransport reports whether gRPC managed tunnel mode should be attempted.
func UseGRPCEdgeTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeEdgeTransport(cfg.EdgeTransport)
	return transport == TransportGRPC || transport == TransportAuto
}

// UseWebSocketEdgeTransport reports whether websocket managed tunnel mode is allowed.
func UseWebSocketEdgeTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeEdgeTransport(cfg.EdgeTransport)
	return transport == TransportWebSocket || transport == TransportAuto
}

// UsePollEdgeTransport reports whether the Portainer-style polling control plane
// should be used.
func UsePollEdgeTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return NormalizeEdgeTransport(cfg.EdgeTransport) == TransportPoll
}

// GetActiveTunnelTransport returns the currently active tunnel transport for an environment.
func GetActiveTunnelTransport(envID string) (string, bool) {
	tunnel, ok := GetRegistry().Get(envID)
	if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
		return "", false
	}

	switch tunnel.Conn.(type) {
	case *GRPCManagerTunnelConn, *GRPCAgentTunnelConn:
		return TransportGRPC, true
	case *TunnelConn:
		return TransportWebSocket, true
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
		state.Transport = TransportGRPC
	case *TunnelConn:
		state.Transport = TransportWebSocket
	}

	state.ConnectedAt = new(tunnel.ConnectedAt)
	state.LastHeartbeat = new(tunnel.GetLastHeartbeat())
	state.SessionID = tunnel.SessionID
	state.AgentInstance = tunnel.AgentInstance
	state.SecurityMode = tunnel.SecurityMode
	state.Capabilities = append([]string(nil), tunnel.Capabilities...)
	state.State = tunnel.State

	return state, true
}
