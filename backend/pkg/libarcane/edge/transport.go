package edge

import (
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/config"
)

const (
	// EdgeTransportWebSocket keeps current JSON-over-WebSocket transport.
	//
	// Deprecated: WebSocket tunnel transport is deprecated. Use EdgeTransportGRPC.
	EdgeTransportWebSocket = "websocket"
	// EdgeTransportGRPC enables protobuf-over-gRPC transport.
	EdgeTransportGRPC = "grpc"
)

// NormalizeEdgeTransport normalizes transport config and defaults to gRPC.
func NormalizeEdgeTransport(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EdgeTransportWebSocket:
		return EdgeTransportWebSocket
	case EdgeTransportGRPC:
		return EdgeTransportGRPC
	default:
		return EdgeTransportGRPC
	}
}

// UseGRPCEdgeTransport reports whether gRPC tunnel transport is enabled.
func UseGRPCEdgeTransport(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	return NormalizeEdgeTransport(cfg.EdgeTransport) == EdgeTransportGRPC
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
