package agent

import (
	"strings"
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
