package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goroutineCount returns the current number of goroutines with a brief settling delay.
func goroutineCount() int {
	// Allow goroutines from prior tests to finish
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	return runtime.NumGoroutine()
}

// pkgPath is the ws package path for stack matching (avoid counting other packages).
const pkgPath = "internal/utils/ws"

// countWsWorkerGoroutines returns the number of goroutines that are running ws package
// workers (Hub.Run, readPump, writePump, ForwardLines, ForwardLogJSONBatched, or
// test-style producer/broadcaster loops). Used to assert zero leak of our code only,
// ignoring httptest/net/http goroutines.
func countWsWorkerGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	s := string(buf[:n])
	count := 0
	blocks := strings.SplitSeq(s, "\n\n")
	for block := range blocks {
		if block == "" || !strings.Contains(block, pkgPath) {
			continue
		}
		// Match our long-lived workers only (not the test runner).
		if strings.Contains(block, ".Run(") ||
			strings.Contains(block, "readPump") ||
			strings.Contains(block, "writePump") ||
			strings.Contains(block, "ForwardLines") ||
			strings.Contains(block, "ForwardLogJSONBatched") ||
			strings.Contains(block, "statsChan") ||
			strings.Contains(block, "hub.Broadcast") {
			count++
		}
	}
	return count
}

// waitForWsWorkerGoroutines waits until the ws worker goroutine count is <= target or timeout.
func waitForWsWorkerGoroutines(t *testing.T, target int, timeout time.Duration) int {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return countWsWorkerGoroutines()
		default:
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
			n := countWsWorkerGoroutines()
			if n <= target {
				return n
			}
		}
	}
}

// waitForGoroutineCount waits for goroutine count to reach target (±tolerance).
func waitForGoroutineCount(t *testing.T, target, tolerance int, timeout time.Duration) int {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			actual := runtime.NumGoroutine()
			return actual
		default:
			runtime.GC()
			actual := runtime.NumGoroutine()
			if actual <= target+tolerance {
				return actual
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ============================================================================
// Core goroutine leak tests: these replicate the pattern from ws_handler.go
// where each page view creates a Hub + streaming goroutines + ServeClient,
// and cleanup depends on the OnEmpty callback firing when the client disconnects.
// ============================================================================

// TestLeak_HubRunExitsOnContextCancel verifies that Hub.Run goroutine exits
// when the context is cancelled (simulates cleanup path).
func TestLeak_HubRunExitsOnContextCancel(t *testing.T) {
	baseline := goroutineCount()

	ctx, cancel := context.WithCancel(context.Background())
	h := NewHub(64)
	go h.Run(ctx)

	// Verify Run is active
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() > baseline
	}, time.Second, 10*time.Millisecond)

	cancel()

	actual := waitForGoroutineCount(t, baseline, 2, 3*time.Second)
	assert.LessOrEqual(t, actual, baseline+2,
		"Hub.Run goroutine should exit after context cancel; goroutine delta: %d", actual-baseline)
}

// TestLeak_ServeClientFullLifecycle verifies zero goroutine leaks from the full
// ServeClient lifecycle: register -> receive messages -> client disconnect -> cleanup.
// This is the core pattern for every WebSocket page in Arcane.
func TestLeak_ServeClientFullLifecycle(t *testing.T) {
	baseline := goroutineCount()

	h := NewHub(64)
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)

	// Set up WebSocket server that calls ServeClient (mirrors ws_handler.go pattern)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ServeClient(ctx, h, conn)
	}))
	defer server.Close()

	// Connect a client
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}

	require.Eventually(t, func() bool {
		return h.ClientCount() == 1
	}, time.Second, 5*time.Millisecond)

	// The client is connected; now we should have extra goroutines:
	// Hub.Run + readPump + writePump = 3 goroutines above the httptest server goroutines
	midpoint := runtime.NumGoroutine()
	assert.Greater(t, midpoint, baseline, "should have active goroutines while connected")

	// Client disconnects (simulates browser close/reload)
	clientConn.Close()

	require.Eventually(t, func() bool {
		return h.ClientCount() == 0
	}, 3*time.Second, 50*time.Millisecond, "client should be removed from hub after close")

	// Cancel context to stop Hub.Run (simulates OnEmpty calling cancel())
	cancel()

	actual := waitForGoroutineCount(t, baseline, 3, 5*time.Second)
	assert.LessOrEqual(t, actual, baseline+3,
		"all goroutines should exit after client disconnect + cancel; delta: %d", actual-baseline)
}

// TestLeak_OnEmptyCallbackCancelsContext verifies the OnEmpty -> cancel() pattern
// used in ws_handler.go actually cleans up all goroutines when the last client leaves.
func TestLeak_OnEmptyCallbackCancelsContext(t *testing.T) {
	baseline := goroutineCount()

	h := NewHub(64)
	ctx, cancel := context.WithCancel(context.Background())

	h.SetOnEmpty(func() {
		cancel() // This is what ws_handler.go does
	})

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

	// Close client -> triggers safeRemove -> hub.remove -> OnEmpty -> cancel()
	clientConn.Close()

	// Hub.Run should exit because OnEmpty called cancel()
	actual := waitForGoroutineCount(t, baseline, 3, 5*time.Second)
	assert.LessOrEqual(t, actual, baseline+3,
		"OnEmpty -> cancel() should clean up Hub.Run + pumps; delta: %d", actual-baseline)
}

// TestLeak_RepeatedConnectDisconnect simulates the exact bug scenario:
// rapidly connecting and disconnecting (like reloading a container page 20+ times).
// Each cycle creates a new Hub + ServeClient (matching the ws_handler.go pattern).
// Goroutine count should remain bounded.
func TestLeak_RepeatedConnectDisconnect(t *testing.T) {
	baseline := goroutineCount()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		hub := NewHub(64)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx)
		ServeClient(ctx, hub, conn)
	}))
	defer server.Close()

	const iterations = 100
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	for i := range iterations {
		conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err, "iteration %d", i)
		if resp != nil {
			resp.Body.Close()
		}

		// Brief moment to let registration happen
		time.Sleep(10 * time.Millisecond)

		// Close (simulates page reload)
		conn.Close()

		// Brief moment for cleanup
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for all ws worker goroutines (Hub.Run, readPump, writePump) to exit.
	wsWorkers := waitForWsWorkerGoroutines(t, 0, 5*time.Second)
	assert.Equal(t, 0, wsWorkers,
		"after %d connect/disconnect cycles, ws worker goroutines must be 0; got %d",
		iterations, wsWorkers)

	// Close the test server so its listener goroutine exits before we measure.
	server.Close()

	actual := waitForGoroutineCount(t, baseline, 0, 3*time.Second)
	leaked := actual - baseline
	assert.Equal(t, 0, leaked,
		"after %d connect/disconnect cycles, global goroutine delta must be 0; "+
			"baseline=%d, final=%d", iterations, baseline, actual)
	t.Logf("goroutine leak check: baseline=%d, final=%d, delta=%d, ws_workers=%d (after %d cycles)",
		baseline, actual, leaked, wsWorkers, iterations)
}

// TestLeak_HubWithForwardLinesLifecycle tests the full pipeline:
// Hub.Run + ForwardLines + ServeClient, all cleaned up when client disconnects.
// This mirrors startContainerLogHub() with format="text".
func TestLeak_HubWithForwardLinesLifecycle(t *testing.T) {
	baseline := goroutineCount()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:contextcheck // intentional: context must outlive the HTTP request for WebSocket streaming
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		hub := NewHub(1024)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx) //nolint:contextcheck // intentional: context must outlive the HTTP request for WebSocket streaming

		// Simulate a streaming data source (like Docker container logs)
		lines := make(chan string, 256)
		go func() {
			defer close(lines)
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					lines <- "log line from container"
				}
			}
		}()

		go ForwardLines(ctx, hub, lines) //nolint:contextcheck // intentional: context must outlive the HTTP request

		ServeClient(ctx, hub, conn) //nolint:contextcheck // intentional: context must outlive the HTTP request
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}

	// Read a few messages to confirm the pipeline works
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = clientConn.ReadMessage()
	require.NoError(t, err)

	// Close client -> OnEmpty -> cancel() -> all goroutines should exit
	clientConn.Close()

	actual := waitForGoroutineCount(t, baseline, 3, 5*time.Second)
	assert.LessOrEqual(t, actual, baseline+3,
		"Hub+ForwardLines+streaming goroutines should all exit; delta: %d", actual-baseline)
}

// TestLeak_HubWithForwardLogJSONBatchedLifecycle tests the batched JSON pipeline:
// Hub.Run + stream producer + normalizer + ForwardLogJSONBatched + ServeClient.
// This mirrors startContainerLogHub() with format="json" and batched=true.
func TestLeak_HubWithForwardLogJSONBatchedLifecycle(t *testing.T) {
	baseline := goroutineCount()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:contextcheck // intentional: context must outlive the HTTP request for WebSocket streaming
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		hub := NewHub(1024)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx) //nolint:contextcheck // intentional: context must outlive the HTTP request

		// Simulate streaming source
		lines := make(chan string, 256)
		go func() {
			defer close(lines)
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					lines <- "2024-01-15T10:30:45.123Z container log line"
				}
			}
		}()

		// Normalizer goroutine (matches ws_handler.go pattern)
		msgs := make(chan LogMessage, 256)
		go func() {
			defer close(msgs)
			seq := uint64(0)
			for line := range lines {
				level, msg, ts := NormalizeContainerLine(line)
				seq++
				if ts == "" {
					ts = NowRFC3339()
				}
				msgs <- LogMessage{
					Seq:       seq,
					Level:     level,
					Message:   msg,
					Timestamp: ts,
				}
			}
		}()

		go ForwardLogJSONBatched(ctx, hub, msgs, 50, 400*time.Millisecond) //nolint:contextcheck // intentional: context must outlive the HTTP request

		ServeClient(ctx, hub, conn) //nolint:contextcheck // intentional: context must outlive the HTTP request
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}

	// Read a message to confirm the full pipeline works
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := clientConn.ReadMessage()
	require.NoError(t, err)

	// Should be a JSON array (batched)
	var batch []LogMessage
	require.NoError(t, json.Unmarshal(raw, &batch))
	assert.NotEmpty(t, batch)

	// Disconnect
	clientConn.Close()

	actual := waitForGoroutineCount(t, baseline, 3, 5*time.Second)
	assert.LessOrEqual(t, actual, baseline+3,
		"full batched JSON pipeline goroutines should all exit; delta: %d", actual-baseline)
}

// startStatsHubForTest starts Hub.Run plus stats producer and JSON broadcaster;
// caller must call ServeClient(ctx, hub, conn). Used by container-stats leak tests.
func startStatsHubForTest(ctx context.Context, hub *Hub) {
	statsChan := make(chan any, 64)
	go func() {
		defer close(statsChan)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case <-ctx.Done():
					return
				case statsChan <- map[string]any{"cpu_percent": 12.5, "memory": 1024000}:
				default:
				}
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case stats, ok := <-statsChan:
				if !ok {
					return
				}
				if b, err := json.Marshal(stats); err == nil {
					hub.Broadcast(b)
				}
			}
		}
	}()
}

// startStatsHubRepeatedTest starts stats producer + broadcaster with 50ms ticker (for repeated cycles test).
func startStatsHubRepeatedTest(ctx context.Context, hub *Hub) {
	statsChan := make(chan any, 64)
	go func() {
		defer close(statsChan)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case <-ctx.Done():
					return
				case statsChan <- map[string]float64{"cpu": 10.0}:
				default:
				}
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case stats, ok := <-statsChan:
				if !ok {
					return
				}
				if b, err := json.Marshal(stats); err == nil {
					hub.Broadcast(b)
				}
			}
		}
	}()
}

// TestLeak_ContainerStatsHubPattern tests the container stats streaming pattern:
// Hub.Run + stats producer + JSON broadcaster + ServeClient.
func TestLeak_ContainerStatsHubPattern(t *testing.T) {
	baseline := goroutineCount()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { //nolint:contextcheck // intentional: context must outlive the HTTP request for WebSocket streaming
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		hub := NewHub(64)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx)                //nolint:contextcheck // intentional: context must outlive the HTTP request
		startStatsHubForTest(ctx, hub) //nolint:contextcheck // intentional: context must outlive the HTTP request
		ServeClient(ctx, hub, conn)    //nolint:contextcheck // intentional: context must outlive the HTTP request
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	if resp != nil {
		resp.Body.Close()
	}
	_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := clientConn.ReadMessage()
	require.NoError(t, err)
	var stats map[string]any
	require.NoError(t, json.Unmarshal(raw, &stats))
	clientConn.Close()

	actual := waitForGoroutineCount(t, baseline, 3, 5*time.Second)
	assert.LessOrEqual(t, actual, baseline+3,
		"container stats hub goroutines should all exit; delta: %d", actual-baseline)
}

// TestLeak_RepeatedStatsHubCycles simulates the exact bug report:
// "Go to the container page. Reload the page 20/30 times."
func TestLeak_RepeatedStatsHubCycles(t *testing.T) {
	baseline := goroutineCount()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		hub := NewHub(64)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx)
		startStatsHubRepeatedTest(ctx, hub)
		ServeClient(ctx, hub, conn)
	}))
	defer server.Close()

	const iterations = 100
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	for i := range iterations {
		conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err, "iteration %d", i)
		if resp != nil {
			resp.Body.Close()
		}

		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, _ = conn.ReadMessage()
		conn.Close()
		time.Sleep(30 * time.Millisecond)
	}

	// Wait for all ws worker goroutines to exit (no blocking on full statsChan).
	wsWorkers := waitForWsWorkerGoroutines(t, 0, 5*time.Second)
	assert.Equal(t, 0, wsWorkers,
		"after %d stats hub cycles, ws worker goroutines must be 0; got %d",
		iterations, wsWorkers)

	// Close the test server so its listener goroutine exits before we measure.
	server.Close()

	actual := waitForGoroutineCount(t, baseline, 0, 3*time.Second)
	leaked := actual - baseline
	assert.Equal(t, 0, leaked,
		"after %d stats hub cycles, global goroutine delta must be 0; "+
			"baseline=%d, final=%d", iterations, baseline, actual)
	t.Logf("stats hub leak check: baseline=%d, final=%d, delta=%d, ws_workers=%d (after %d cycles)",
		baseline, actual, leaked, wsWorkers, iterations)
}

// TestLeak_MultipleClientsOnSameHub verifies that multiple clients connecting
// to the same hub don't leak goroutines when they all disconnect.
func TestLeak_MultipleClientsOnSameHub(t *testing.T) {
	baseline := goroutineCount()

	h := NewHub(64)
	ctx, cancel := context.WithCancel(context.Background())

	h.SetOnEmpty(func() {
		cancel()
	})

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
	const numClients = 10
	var clients []*websocket.Conn

	for range numClients {
		conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err)
		if resp != nil {
			resp.Body.Close()
		}
		clients = append(clients, conn)
	}

	require.Eventually(t, func() bool {
		return h.ClientCount() == numClients
	}, 2*time.Second, 10*time.Millisecond)

	// Close all clients
	for _, c := range clients {
		c.Close()
	}

	// OnEmpty should fire after last client leaves, cancelling context
	actual := waitForGoroutineCount(t, baseline, 3, 5*time.Second)
	assert.LessOrEqual(t, actual, baseline+3,
		"all client goroutines + Hub.Run should exit; delta: %d", actual-baseline)
}
