package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestWSPair creates a connected client/server WebSocket pair using httptest.
func newTestWSPair(t *testing.T) (clientConn *websocket.Conn, serverConn *websocket.Conn, cleanup func()) {
	t.Helper()
	serverReady := make(chan *websocket.Conn, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverReady <- conn
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	cc, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}

	sc := <-serverReady

	return cc, sc, func() {
		cc.Close()
		sc.Close()
		server.Close()
	}
}

func TestNewHub(t *testing.T) {
	h := NewHub(10)
	require.NotNil(t, h)
	assert.Equal(t, 0, h.ClientCount())
}

func TestHub_RegisterAndClientCount(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 16)
	h.register <- c

	// Wait for registration to process
	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)
}

func TestHub_UnregisterClient(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 16)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	h.unregister <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, time.Second, 5*time.Millisecond)
}

func TestHub_Broadcast(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 16)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	msg := []byte("hello world")
	h.Broadcast(msg)

	// Message should appear on the client's send channel
	select {
	case received := <-c.send:
		assert.Equal(t, msg, received)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast message")
	}
}

func TestHub_BroadcastToMultipleClients(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	const numClients = 5
	clients := make([]*Client, numClients)
	cleanups := make([]func(), numClients)

	for i := range numClients {
		_, sc, cleanup := newTestWSPair(t)
		cleanups[i] = cleanup
		clients[i] = NewClient(sc, 16)
		h.register <- clients[i]
	}
	defer func() {
		for _, fn := range cleanups {
			fn()
		}
	}()

	require.Eventually(t, func() bool {
		return h.ClientCount() == numClients
	}, time.Second, 5*time.Millisecond)

	msg := []byte("broadcast to all")
	h.Broadcast(msg)

	for i, c := range clients {
		select {
		case received := <-c.send:
			assert.Equal(t, msg, received, "client %d did not receive expected message", i)
		case <-time.After(time.Second):
			t.Fatalf("client %d timed out waiting for broadcast message", i)
		}
	}
}

func TestHub_BackpressureDropsSlowClient(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	// Create a client with a tiny send buffer so it fills up fast
	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 1)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Fill the client's send buffer
	c.send <- []byte("fill")

	// Now broadcast — the client's channel is full, so it should get dropped
	h.Broadcast([]byte("overflow"))

	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, time.Second, 5*time.Millisecond)
}

func TestHub_OnEmptyCallback(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	var called atomic.Bool
	h.SetOnEmpty(func() {
		called.Store(true)
	})

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 16)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	h.unregister <- c

	require.Eventually(t, called.Load, time.Second, 5*time.Millisecond, "onEmpty callback was not called")
}

func TestHub_OnEmptyCalledOnlyOnce(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	var callCount atomic.Int32
	h.SetOnEmpty(func() {
		callCount.Add(1)
	})

	// Register two clients
	_, sc1, cleanup1 := newTestWSPair(t)
	defer cleanup1()
	_, sc2, cleanup2 := newTestWSPair(t)
	defer cleanup2()

	c1 := NewClient(sc1, 16)
	c2 := NewClient(sc2, 16)
	h.register <- c1
	h.register <- c2

	require.Eventually(t, func() bool {
		return h.ClientCount() == 2
	}, time.Second, 5*time.Millisecond)

	// Unregister first — should NOT trigger onEmpty
	h.unregister <- c1
	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), callCount.Load(), "onEmpty should not fire while clients remain")

	// Unregister last — should trigger onEmpty exactly once
	h.unregister <- c2
	require.Eventually(t, func() bool {
		return callCount.Load() == 1
	}, time.Second, 5*time.Millisecond)
}

func TestHub_ContextCancellation(t *testing.T) {
	h := NewHub(10)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 16)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	cancel()

	select {
	case <-done:
		// Hub exited as expected
	case <-time.After(2 * time.Second):
		t.Fatal("Hub.Run did not exit after context cancellation")
	}

	// After closeAll, clients map should be empty
	assert.Equal(t, 0, h.ClientCount())
}

func TestHub_BroadcastBufferFull(t *testing.T) {
	// Hub with a buffer of 1
	h := NewHub(1)
	ctx := t.Context()

	// Don't run the hub — the broadcast channel will fill up
	h.broadcast <- []byte("fill")

	// This should not block (drops the message)
	h.Broadcast([]byte("should be dropped"))

	// Verify no panic or deadlock occurred
	go h.Run(ctx)
}

func TestHub_ConcurrentOperations(t *testing.T) {
	h := NewHub(100)
	ctx := t.Context()
	go h.Run(ctx)

	const goroutines = 10
	var wg sync.WaitGroup

	// Create all pairs in the test goroutine (newTestWSPair uses t).
	pairs := make([]struct {
		sc      *websocket.Conn
		cleanup func()
	}, goroutines)
	for i := range goroutines {
		_, pairs[i].sc, pairs[i].cleanup = newTestWSPair(t)
		defer pairs[i].cleanup()
	}

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sc := pairs[idx].sc
			c := NewClient(sc, 16)
			h.register <- c

			time.Sleep(10 * time.Millisecond)
			h.Broadcast([]byte("concurrent message"))
			time.Sleep(10 * time.Millisecond)
			h.unregister <- c
		}(i)
	}

	wg.Wait()

	// All clients should be gone
	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestHub_DoubleUnregister(t *testing.T) {
	h := NewHub(10)
	ctx := t.Context()
	go h.Run(ctx)

	_, serverConn, cleanup := newTestWSPair(t)
	defer cleanup()

	c := NewClient(serverConn, 16)
	h.register <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// Unregister twice — should not panic
	h.unregister <- c

	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, time.Second, 5*time.Millisecond)

	// Second unregister should be harmless
	h.unregister <- c
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, h.ClientCount())
}
