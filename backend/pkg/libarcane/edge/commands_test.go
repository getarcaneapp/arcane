package edge

import (
	"context"
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
		{name: "health", method: "HEAD", path: "/api/environments/0/system/health", command: "system.health", shouldHit: true},
		{name: "unknown", method: "PATCH", path: "/api/environments/0/containers", shouldHit: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			command, ok := ResolveEdgeCommandName(tc.method, tc.path, tc.stream)
			require.Equal(t, tc.shouldHit, ok)
			if tc.shouldHit {
				require.Equal(t, tc.command, command)
				require.True(t, ValidateEdgeCommand(tc.command, tc.method, tc.path, tc.stream))
			}
		})
	}
}

func TestCollectCommandResponse(t *testing.T) {
	t.Parallel()

	respCh := make(chan *TunnelMessage, 4)
	respCh <- &TunnelMessage{ID: "cmd-1", Type: MessageTypeCommandAck}
	respCh <- &TunnelMessage{ID: "cmd-1", Type: MessageTypeCommandOutput, Body: []byte("hello ")}
	respCh <- &TunnelMessage{ID: "cmd-1", Type: MessageTypeCommandComplete, Status: 200, Headers: map[string]string{"Content-Type": "text/plain"}, Body: []byte("world")}

	status, headers, body, err := collectCommandResponseInternal(context.Background(), respCh)
	require.NoError(t, err)
	require.Equal(t, 200, status)
	require.Equal(t, "text/plain", headers["Content-Type"])
	require.Equal(t, "hello world", string(body))
}
