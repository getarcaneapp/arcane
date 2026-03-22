package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ws "github.com/getarcaneapp/arcane/backend/pkg/libarcane/ws"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestBroadcastContainerLogStreamErrorInternal_JSON(t *testing.T) {
	hub := ws.NewHub(10)
	ctx := t.Context()
	go hub.Run(ctx)

	clientConn, serverConn, cleanup := newTestWSPairInternal(t)
	t.Cleanup(cleanup)

	ws.ServeClient(ctx, hub, serverConn)

	stream := &wsLogStream{
		hub:    hub,
		format: "json",
	}

	broadcastContainerLogStreamErrorInternal(stream, errors.New("boom"))

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := clientConn.ReadMessage()
	require.NoError(t, err)

	var message ws.LogMessage
	require.NoError(t, json.Unmarshal(payload, &message))
	require.Equal(t, "error", message.Level)
	require.Equal(t, "Failed to stream container logs: boom", message.Message)
	require.Empty(t, message.ContainerID)
	require.NotEmpty(t, message.Timestamp)
}

func TestBroadcastContainerLogStreamErrorInternal_Text(t *testing.T) {
	hub := ws.NewHub(10)
	ctx := t.Context()
	go hub.Run(ctx)

	clientConn, serverConn, cleanup := newTestWSPairInternal(t)
	t.Cleanup(cleanup)

	ws.ServeClient(ctx, hub, serverConn)

	stream := &wsLogStream{
		hub:    hub,
		format: "text",
	}

	broadcastContainerLogStreamErrorInternal(stream, errors.New("boom"))

	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "Failed to stream container logs: boom", string(payload))
}

func newTestWSPairInternal(t *testing.T) (clientConn *websocket.Conn, serverConn *websocket.Conn, cleanup func()) {
	t.Helper()
	serverReady := make(chan *websocket.Conn, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverReady <- conn
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}

	serverConn = <-serverReady

	return clientConn, serverConn, func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		server.Close()
	}
}
