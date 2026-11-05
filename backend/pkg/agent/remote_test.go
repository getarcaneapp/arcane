package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMockAgentServer creates a WS server that acts as an agent serving the multiplexed data plane with handler.
func setupMockAgentServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *AgentTunnel) {
	agent := &TunnelClient{streamWindowMiB: testStreamWindowMiB, handler: handler}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		_ = agent.serveMuxSession(r.Context(), NewWebSocketTunnelConn(conn).muxConn(r.Context()), testStreamWindowMiB())
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	tunnel := newWebSocketAgentTunnel("env-1", conn)
	go func() {
		_ = (&TunnelServer{streamWindowMiB: testStreamWindowMiB}).serveMuxTunnel(t.Context(), tunnel, tunnel.Conn.muxConn(t.Context()))
	}()

	return server, tunnel
}

func TestRedactedTokenFingerprint(t *testing.T) {
	require.Equal(t, "***", RedactedTokenFingerprint(""))
	require.Equal(t, "***", RedactedTokenFingerprint("short"))
	require.Equal(t, "***", RedactedTokenFingerprint("1234567890"))
	require.Equal(t, "123456...cdef", RedactedTokenFingerprint(" 1234567890abcdef "))
}

func TestTunnelRegistryDo_AgentWithTunnel(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/health", r.URL.Path)
		assert.Equal(t, "v", r.Header.Get("X-H"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Test", "value")
		_, _ = w.Write([]byte(`{"agent":true}`))
	})
	defer server.Close()
	defer func() { _ = tunnel.CloseWithReason("") }()

	tunnels := NewTunnelRegistry()
	envID := tunnel.EnvironmentID
	registerTestTunnel(t, tunnels, tunnel)
	defer tunnels.Unregister(envID)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	resp, err := tunnels.Do(ctx, RemoteRequest{
		Headers:       http.Header{"X-H": {"v"}},
		EnvironmentID: envID,
		Method:        http.MethodGet,
		Path:          "/api/health",
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "value", resp.Headers["X-Test"])
	assert.Equal(t, []byte(`{"agent":true}`), resp.Body)
}

func TestTunnelRegistryDo_AgentNoTunnel(t *testing.T) {
	_, err := NewTunnelRegistry().Do(t.Context(), RemoteRequest{
		EnvironmentID: "env-agent-missing",
		Method:        http.MethodGet,
		Path:          "/api/health",
	})

	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	assert.Contains(t, err.Error(), "not connected")
	assert.True(t, transportErr.Unavailable())
}

func TestTransportErrorUnavailable(t *testing.T) {
	for _, test := range []struct {
		cause       error
		name        string
		unavailable bool
	}{
		{name: "not connected", cause: ErrEnvironmentUnavailable, unavailable: true},
		{name: "deadline", cause: context.DeadlineExceeded, unavailable: true},
		{name: "response truncated", cause: io.ErrUnexpectedEOF},
		{name: "invalid request", cause: errors.New("invalid URL")},
		{name: "canceled", cause: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.unavailable, (&TransportError{Err: test.cause}).Unavailable())
		})
	}
}
