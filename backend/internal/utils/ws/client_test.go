package ws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 64)
	require.NotNil(t, c)
	assert.NotNil(t, c.conn)
	assert.NotNil(t, c.send)
	assert.Equal(t, 64, cap(c.send))
}

func TestServeClient_ReceivesBroadcast(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	// Set up a test WS server that upgrades and serves the client
	serverReady := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		close(serverReady)
		ServeClient(ctx, h, conn)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	<-serverReady

	// Wait for registration
	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Broadcast a message
	h.Broadcast([]byte("test message"))

	// Client should receive it
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := clientConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "test message", string(msg))
}

func TestServeClient_ClientDisconnect(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ServeClient(ctx, h, conn)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Close the client connection
	clientConn.Close()

	// The hub should eventually remove the client
	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, 5*time.Second, 50*time.Millisecond)
}

func TestServeClient_ContextCancellation(t *testing.T) {
	h := NewHub(10)
	hubCtx := t.Context()
	go h.Run(hubCtx)

	clientCtx, clientCancel := context.WithCancel(context.Background())

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ServeClient(clientCtx, h, conn)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Cancel the client context
	clientCancel()

	// Client should be removed from hub
	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, 5*time.Second, 50*time.Millisecond)
}

func TestIsExpectedCloseError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: true,
		},
		{
			name:     "normal closure",
			err:      &websocket.CloseError{Code: websocket.CloseNormalClosure},
			expected: true,
		},
		{
			name:     "going away",
			err:      &websocket.CloseError{Code: websocket.CloseGoingAway},
			expected: true,
		},
		{
			name:     "no status received",
			err:      &websocket.CloseError{Code: websocket.CloseNoStatusReceived},
			expected: true,
		},
		{
			name:     "use of closed network connection",
			err:      errors.New("read tcp: use of closed network connection"),
			expected: true,
		},
		{
			name:     "connection reset by peer",
			err:      errors.New("read: connection reset by peer"),
			expected: true,
		},
		{
			name:     "broken pipe",
			err:      errors.New("write: broken pipe"),
			expected: true,
		},
		{
			name:     "unexpected error",
			err:      errors.New("some unexpected error"),
			expected: false,
		},
		{
			name:     "protocol error",
			err:      &websocket.CloseError{Code: websocket.CloseProtocolError},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExpectedCloseError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServeClient_MultipleMessages(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	serverReady := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		close(serverReady)
		ServeClient(ctx, h, conn)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	defer clientConn.Close()

	<-serverReady

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Send multiple messages and verify order
	messages := []string{"first", "second", "third", "fourth", "fifth"}
	for _, m := range messages {
		h.Broadcast([]byte(m))
	}

	_ = clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for _, expected := range messages {
		_, msg, err := clientConn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, expected, string(msg))
	}
}
