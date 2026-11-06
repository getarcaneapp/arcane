package agent

import (
	"strings"
)

const (
	TransportAuto      = "auto"
	TransportWebSocket = "websocket"
	TransportGRPC      = "grpc"

	MTLSModeDisabled = "disabled"
	MTLSModeOptional = "optional"
	MTLSModeRequired = "required"
)

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
