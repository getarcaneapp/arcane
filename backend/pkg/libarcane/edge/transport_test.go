package edge

import (
	"context"
	"sync"
	"testing"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeEdgeTransport(t *testing.T) {
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport(""))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("grpc"))
	assert.Equal(t, EdgeTransportGRPC, NormalizeEdgeTransport("GRPC"))
	assert.Equal(t, EdgeTransportWebSocket, NormalizeEdgeTransport("websocket"))
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport("invalid"))
	assert.Equal(t, EdgeTransportAuto, NormalizeEdgeTransport("auto"))
}

func TestUseGRPCEdgeTransport(t *testing.T) {
	assert.False(t, UseGRPCEdgeTransport(nil))
	assert.True(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: "grpc"}))
	assert.True(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: ""}))
	assert.True(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: "auto"}))
	assert.False(t, UseGRPCEdgeTransport(&config.Config{EdgeTransport: "websocket"}))
}

func TestUseWebSocketEdgeTransport(t *testing.T) {
	assert.False(t, UseWebSocketEdgeTransport(nil))
	assert.True(t, UseWebSocketEdgeTransport(&config.Config{EdgeTransport: ""}))
	assert.True(t, UseWebSocketEdgeTransport(&config.Config{EdgeTransport: "auto"}))
	assert.False(t, UseWebSocketEdgeTransport(&config.Config{EdgeTransport: "grpc"}))
	assert.True(t, UseWebSocketEdgeTransport(&config.Config{EdgeTransport: "websocket"}))
}

func TestGetActiveTunnelTransport(t *testing.T) {
	t.Run("returns false when tunnel is missing", func(t *testing.T) {
		transport, ok := GetActiveTunnelTransport("env-missing")
		assert.False(t, ok)
		assert.Equal(t, "", transport)
	})

	t.Run("detects grpc tunnel", func(t *testing.T) {
		envID := "env-transport-grpc"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(nil))
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.True(t, ok)
		assert.Equal(t, EdgeTransportGRPC, transport)
	})

	t.Run("detects websocket tunnel", func(t *testing.T) {
		envID := "env-transport-ws"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		conn := createTestConn(t)
		defer func() { _ = conn.Close() }()

		tunnel := NewAgentTunnel(envID, conn)
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.True(t, ok)
		assert.Equal(t, EdgeTransportWebSocket, transport)
	})

	t.Run("returns false for closed tunnel", func(t *testing.T) {
		envID := "env-transport-closed"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, NewGRPCManagerTunnelConn(nil))
		_ = tunnel.Conn.Close()
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.False(t, ok)
		assert.Equal(t, "", transport)
	})

	t.Run("returns false for unknown transport implementation", func(t *testing.T) {
		envID := "env-transport-unknown"
		GetRegistry().Unregister(envID)
		defer GetRegistry().Unregister(envID)

		tunnel := NewAgentTunnelWithConn(envID, &unknownTunnelConn{})
		GetRegistry().Register(envID, tunnel)

		transport, ok := GetActiveTunnelTransport(envID)
		assert.False(t, ok)
		assert.Equal(t, "", transport)
	})
}

type unknownTunnelConn struct{}

func (u *unknownTunnelConn) Send(msg *TunnelMessage) error { return nil }

func (u *unknownTunnelConn) Receive() (*TunnelMessage, error) { return nil, nil }

func (u *unknownTunnelConn) IsExpectedReceiveError(error) bool { return false }

func (u *unknownTunnelConn) Close() error { return nil }

func (u *unknownTunnelConn) IsClosed() bool { return false }

func (u *unknownTunnelConn) SendRequest(ctx context.Context, msg *TunnelMessage, pending *sync.Map) (*TunnelMessage, error) {
	return nil, nil
}
