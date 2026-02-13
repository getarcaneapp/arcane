package edge

import (
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeEdgeTransport(t *testing.T) {
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport(""))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("grpc"))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("GRPC"))
	assert.Equal(t, EdgeTransportWebSocket, NormalizeEdgeTransport("websocket"))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("invalid"))
}

func TestUseGRPCEdgeTransport(t *testing.T) {
	assert.False(t, UseGRPCEdgeTransport(nil))
	assert.True(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: "grpc"}))
	assert.True(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: ""}))
	assert.False(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: "websocket"}))
}
