package edge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveEdgeCommandName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		method    string
		path      string
		stream    bool
		command   string
		shouldHit bool
	}{
		{name: "container list", method: "GET", path: "/api/environments/0/containers", command: "container.list", shouldHit: true},
		{name: "container start", method: "POST", path: "/api/environments/0/containers/abc/start", command: "container.start", shouldHit: true},
		{name: "volume browse download", method: "GET", path: "/api/environments/0/volumes/data/browse/download", command: "volume.browse.download", shouldHit: true},
		{name: "project logs stream", method: "GET", path: "/api/environments/0/ws/projects/p1/logs", stream: true, command: "project.logs.stream", shouldHit: true},
		{name: "project updates", method: "GET", path: "/api/environments/0/projects/p1/updates", command: "project.updates", shouldHit: true},
		{name: "project archive", method: "POST", path: "/api/environments/0/projects/p1/archive", command: "project.archive", shouldHit: true},
		{name: "activity list", method: "GET", path: "/api/environments/0/activities?limit=50", command: "activity.list", shouldHit: true},
		{name: "activity inspect", method: "GET", path: "/api/environments/0/activities/activity-1", command: "activity.inspect", shouldHit: true},
		{name: "activity cancel", method: "POST", path: "/api/environments/0/activities/activity-1/cancel", command: "activity.cancel", shouldHit: true},
		{name: "activity history clear", method: "DELETE", path: "/api/environments/0/activities/history", command: "activity.history.clear", shouldHit: true},
		{name: "health", method: "HEAD", path: "/api/environments/0/system/health", command: "system.health", shouldHit: true},
		{name: "swarm node identity", method: "GET", path: "/api/swarm/node-identity", command: "swarm.node_identity", shouldHit: true},
		{name: "ports list", method: "GET", path: "/api/environments/0/ports?limit=20", command: "port.list", shouldHit: true},
		{name: "network topology", method: "GET", path: "/api/environments/0/networks/topology", command: "network.topology", shouldHit: true},
		{name: "container auto-update", method: "PUT", path: "/api/environments/0/containers/abc/auto-update", command: "container.auto_update.set", shouldHit: true},
		{name: "project update services", method: "POST", path: "/api/environments/0/projects/p1/update-services", command: "project.update_services", shouldHit: true},
		{name: "swarm services list", method: "GET", path: "/api/environments/0/swarm/services", command: "swarm.service.list", shouldHit: true},
		{name: "unknown", method: "PATCH", path: "/api/environments/0/containers", shouldHit: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			command, ok := ResolveEdgeCommandName(tc.method, tc.path, tc.stream).Get()
			require.Equal(t, tc.shouldHit, ok)
			if tc.shouldHit {
				require.Equal(t, tc.command, command)
				require.True(t, ValidateEdgeCommand(tc.command, tc.method, tc.path, tc.stream))
			}
		})
	}
}

func TestBuildCommandRouteIndexInternalPanicsOnDuplicateRoute(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		buildCommandRouteIndexInternal([]commandRoute{
			{Method: http.MethodGet, PathPattern: "/api/test/{id}", CommandName: "test.one"},
			{Method: http.MethodGet, PathPattern: "/api/test/{id}", CommandName: "test.two"},
		})
	})
}

func TestCollectCommandResponse(t *testing.T) {
	t.Parallel()

	tunnel := NewAgentTunnelWithConn("env-1", &fakeServerTunnelConn{})
	pending := &PendingRequest{
		ResponseCh: make(chan *TunnelMessage, 4),
		failureCh:  make(chan error, 1),
	}
	pending.ResponseCh <- &TunnelMessage{ID: "cmd-1", Type: MessageTypeCommandAck}
	pending.ResponseCh <- &TunnelMessage{ID: "cmd-1", Type: MessageTypeCommandOutput, Body: []byte("hello ")}
	pending.ResponseCh <- &TunnelMessage{ID: "cmd-1", Type: MessageTypeCommandComplete, Status: 200, Headers: map[string]string{"Content-Type": "text/plain"}, Body: []byte("world")}
	require.NoError(t, tunnel.CloseWithReason(""))

	status, headers, body, streamed, err := collectCommandResponseInternal(context.Background(), tunnel, pending, "", nil)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, "text/plain", headers["Content-Type"])
	require.Equal(t, "hello world", string(body))
	require.False(t, streamed)
}

func TestCollectCommandResponseRejectsOversizedBody(t *testing.T) {
	testCases := []struct {
		name     string
		messages []*TunnelMessage
	}{
		{
			name: "streamed response",
			messages: []*TunnelMessage{
				{Type: MessageTypeResponse, Status: http.StatusOK, Headers: map[string]string{"X-Arcane-Tunnel-Stream": "1"}, Body: make([]byte, maxProxyResponseBodySize)},
				{Type: MessageTypeStreamData, Body: []byte{1}},
			},
		},
		{
			name: "command output",
			messages: []*TunnelMessage{
				{Type: MessageTypeCommandOutput, Body: make([]byte, maxProxyResponseBodySize)},
				{Type: MessageTypeCommandComplete, Status: http.StatusOK, Body: []byte{1}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tunnel := NewAgentTunnelWithConn("env-oversized", &fakeServerTunnelConn{})
			defer func() { require.NoError(t, tunnel.CloseWithReason("")) }()

			pending := &PendingRequest{
				ResponseCh: make(chan *TunnelMessage, len(tc.messages)),
				failureCh:  make(chan error, 1),
			}
			for _, message := range tc.messages {
				pending.ResponseCh <- message
			}

			status, headers, body, streamed, err := collectCommandResponseInternal(t.Context(), tunnel, pending, http.MethodGet, nil)
			require.ErrorContains(t, err, "edge tunnel response body exceeds limit")
			require.Zero(t, status)
			require.Nil(t, headers)
			require.Nil(t, body)
			require.False(t, streamed)
		})
	}
}

func TestCollectCommandResponseStreamsBodyPastBufferedLimit(t *testing.T) {
	tunnel := NewAgentTunnelWithConn("env-streamed", &fakeServerTunnelConn{})
	defer func() { require.NoError(t, tunnel.CloseWithReason("")) }()

	pending := &PendingRequest{
		ResponseCh: make(chan *TunnelMessage, 4),
		failureCh:  make(chan error, 1),
	}
	firstChunk := make([]byte, maxProxyResponseBodySize)
	lastChunk := []byte("complete")
	pending.ResponseCh <- &TunnelMessage{
		Type:    MessageTypeResponse,
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/octet-stream", "X-Arcane-Tunnel-Stream": "1"},
	}
	pending.ResponseCh <- &TunnelMessage{Type: MessageTypeCommandOutput, Body: firstChunk}
	pending.ResponseCh <- &TunnelMessage{Type: MessageTypeCommandOutput, Body: lastChunk}
	pending.ResponseCh <- &TunnelMessage{Type: MessageTypeCommandComplete, Status: http.StatusOK, Streaming: true}

	w := httptest.NewRecorder()
	status, headers, body, streamed, err := collectCommandResponseInternal(t.Context(), tunnel, pending, http.MethodGet, w)
	require.NoError(t, err)
	require.True(t, streamed)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "application/octet-stream", headers["Content-Type"])
	require.Nil(t, body)
	require.Equal(t, len(firstChunk)+len(lastChunk), w.Body.Len())
	require.Equal(t, "", w.Header().Get("X-Arcane-Tunnel-Stream"))
}
