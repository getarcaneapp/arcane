package edge

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	tunnelpb "github.com/getarcaneapp/arcane/backend/proto/tunnel/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestTunnelClient_GRPC_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	envID := "env-e2e-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	grpcServer := grpc.NewServer()
	tunnelpb.RegisterTunnelServiceServer(grpcServer, tunnelServer)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = lis.Close() }()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/local/health" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-from-agent"))
	})

	cfg := &config.Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         "http://" + lis.Addr().String(),
		AgentToken:            "valid-token",
		EdgeReconnectInterval: 1,
		Port:                  "3552",
	}

	client := NewTunnelClient(cfg, localHandler)
	errCh := make(chan error, 4)
	go client.StartWithErrorChan(ctx, errCh)

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, 5*time.Second, 20*time.Millisecond)

	proxyCtx, proxyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer proxyCancel()

	status, headers, body, err := ProxyRequest(proxyCtx, tunnel, http.MethodGet, "/local/health", "", map[string]string{"Accept": "text/plain"}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "text/plain", headers["Content-Type"])
	assert.Equal(t, "ok-from-agent", string(body))

	select {
	case clientErr := <-errCh:
		require.NoError(t, clientErr)
	default:
	}
}
