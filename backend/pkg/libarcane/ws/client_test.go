package ws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		close(serverReady)
		ServeClientWithOnRemove(ctx, h, conn, nil)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	<-serverReady

	// Wait for registration
	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Broadcast a message
	h.Broadcast([]byte("test message"))

	// Client should receive it
	readCtx, readCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer readCancel()
	_, msg, err := clientConn.Read(readCtx)
	require.NoError(t, err)
	assert.Equal(t, "test message", string(msg))
}

func TestServeClient_ClientDisconnect(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ServeClientWithOnRemove(ctx, h, conn, nil)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Close the client connection
	_ = clientConn.CloseNow()

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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ServeClientWithOnRemove(clientCtx, h, conn, nil)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

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
			err:      websocket.CloseError{Code: websocket.StatusNormalClosure},
			expected: true,
		},
		{
			name:     "going away",
			err:      websocket.CloseError{Code: websocket.StatusGoingAway},
			expected: true,
		},
		{
			name:     "no status received",
			err:      websocket.CloseError{Code: websocket.StatusNoStatusRcvd},
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
			err:      websocket.CloseError{Code: websocket.StatusProtocolError},
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		close(serverReady)
		ServeClientWithOnRemove(ctx, h, conn, nil)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	<-serverReady

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Send multiple messages and verify order
	messages := []string{"first", "second", "third", "fourth", "fifth"}
	for _, m := range messages {
		h.Broadcast([]byte(m))
	}

	readCtx, readCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer readCancel()
	for _, expected := range messages {
		_, msg, err := clientConn.Read(readCtx)
		require.NoError(t, err)
		assert.Equal(t, expected, string(msg))
	}
}

// A reconnect can land on a hub whose Run has already exited (e.g. the idle
// teardown fired between lookup and registration). That used to block forever
// on the unbuffered register channel, leaking the caller's goroutine and the
// socket; registration must now fail so the caller can retry with a fresh hub.
func TestServeClient_StoppedHubDoesNotBlock(t *testing.T) {
	h := NewHub(10)
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		h.Run(ctx)
	}()

	cancel()
	<-stopped

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	var onRemoveCalled atomic.Bool
	done := make(chan bool, 1)
	go func() {
		done <- ServeClientWithOnRemove(t.Context(), h, serverConn, func() {
			onRemoveCalled.Store(true)
		})
	}()

	select {
	case registered := <-done:
		assert.False(t, registered, "registration against a stopped hub must fail")
	case <-time.After(2 * time.Second):
		t.Fatal("ServeClientWithOnRemove blocked on a stopped hub")
	}

	// The caller owns the connection and its bookkeeping on failure, so it can
	// retry the same conn against a fresh hub.
	assert.False(t, onRemoveCalled.Load(), "onRemove must not run when registration fails")
	assert.Equal(t, 0, h.ClientCount())
}
