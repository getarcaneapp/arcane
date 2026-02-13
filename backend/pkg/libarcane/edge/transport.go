package edge

import (
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/config"
)

const (
	// EdgeTransportWebSocket forces WebSocket tunnel transport.
	EdgeTransportWebSocket = "websocket"
	// EdgeTransportGRPC forces gRPC transport without WebSocket fallback.
	EdgeTransportGRPC = "grpc"
	// EdgeTransportAuto prefers gRPC and falls back to WebSocket automatically.
	EdgeTransportAuto = "auto"
)

// NormalizeEdgeTransport normalizes transport config and defaults to auto-negotiation.
func NormalizeEdgeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EdgeTransportWebSocket:
		return EdgeTransportWebSocket
	case EdgeTransportGRPC:
		return EdgeTransportGRPC
	case EdgeTransportAuto:
		return EdgeTransportAuto
	default:
		return EdgeTransportAuto
	}
}

// UseGRPCEdgeTransport reports whether gRPC should be attempted.
func UseGRPCEdgeTransport(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeEdgeTransport(cfg.EdgeTransport)
	return transport == EdgeTransportGRPC || transport == EdgeTransportAuto
}

// UseWebSocketEdgeTransport reports whether WebSocket transport is allowed.
func UseWebSocketEdgeTransport(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	transport := NormalizeEdgeTransport(cfg.EdgeTransport)
	return transport == EdgeTransportWebSocket || transport == EdgeTransportAuto
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
