package agent

import (
	"strings"
	"time"

	"github.com/samber/mo"
	kit "go.getarcane.app/kit/pkg"
)

const (
	// TransportAuto prefers gRPC and falls back to WebSocket automatically.
	TransportAuto = "auto"
	// TransportWebSocket forces WebSocket tunnel transport.
	TransportWebSocket = "websocket"
	// TransportGRPC forces gRPC transport without WebSocket fallback.
	TransportGRPC = "grpc"

	// MTLSModeDisabled disables agent tunnel mTLS.
	MTLSModeDisabled = "disabled"
	// MTLSModeOptional enables agent tunnel mTLS when certificates are configured.
	MTLSModeOptional = "optional"
	// MTLSModeRequired requires a verified client certificate on agent tunnel endpoints
	// when Arcane terminates TLS; external TLS terminators must enforce mTLS before proxying.
	MTLSModeRequired = "required"
)

// NormalizeTransport normalizes transport config and defaults to auto-negotiation.
func NormalizeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case TransportAuto:
		return TransportAuto
	case TransportWebSocket:
		return TransportWebSocket
	case TransportGRPC:
		return TransportGRPC
	default:
		return TransportAuto
	}
}

// NormalizeMTLSMode normalizes agent mTLS config and defaults to disabled.
func NormalizeMTLSMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MTLSModeOptional:
		return MTLSModeOptional
	case MTLSModeRequired:
		return MTLSModeRequired
	default:
		return MTLSModeDisabled
	}
}

// UseGRPCTransport reports whether gRPC managed tunnel mode should be attempted.
func UseGRPCTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeTransport(cfg.AgentTransport)
	return transport == TransportGRPC || transport == TransportAuto
}

// UseWebSocketTransport reports whether websocket managed tunnel mode is allowed.
func UseWebSocketTransport(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeTransport(cfg.AgentTransport)
	return transport == TransportWebSocket || transport == TransportAuto
}

// GetTunnelRuntimeState returns live metadata for an active tunnel.
func GetTunnelRuntimeState(envID string) mo.Option[*TunnelRuntimeState] {
	tunnel, ok := GetActiveTunnel(envID).Get()
	if !ok {
		return mo.None[*TunnelRuntimeState]()
	}
	// One snapshot under a single read lock keeps the fields consistent and race-free.
	meta := tunnel.MetadataSnapshot()

	rtt := time.Duration(tunnel.mux.rtt.Load())
	return mo.Some(&TunnelRuntimeState{
		RoundTripTime: kit.Ternary(rtt > 0, &rtt, nil),
		OpenStreams:   tunnel.openStreams(),
		Transport:     tunnel.Conn.Transport(),
		ConnectedAt:   new(meta.ConnectedAt),
		LastHeartbeat: new(meta.LastHeartbeat),
		SessionID:     meta.SessionID,
		AgentInstance: meta.AgentInstance,
		SecurityMode:  meta.SecurityMode,
		Capabilities:  meta.Capabilities,
		State:         meta.State,
	})
}
