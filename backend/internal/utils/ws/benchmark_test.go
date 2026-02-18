package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// benchWSServer creates a test WebSocket server for benchmarks.
// The server keeps connections open until the returned cleanup is called.
func benchWSServer(b *testing.B) (url string, cleanup func()) {
	b.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		go func() {
			// Drain reads until context cancelled
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		<-ctx.Done()
		conn.Close()
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return wsURL, func() {
		cancel()
		server.Close()
	}
}

// benchHub creates a hub with N connected clients whose send channels are drained.
// Returns the hub, a context cancel func, and a cleanup func.
func benchHub(b *testing.B, numClients int, hubBuf int) (*Hub, context.CancelFunc, func()) {
	b.Helper()

	h := NewHub(hubBuf)
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)

	wsURL, serverCleanup := benchWSServer(b)

	var conns []*websocket.Conn
	for range numClients {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatal(err)
		}
		if resp != nil {
			resp.Body.Close()
		}
		conns = append(conns, conn)

		c := NewClient(conn, 4096)
		h.register <- c

		// Drain send channel to simulate fast client
		go func() {
			for range c.send {
			}
		}()
	}

	// Wait for all clients to register
	for h.ClientCount() < numClients {
		time.Sleep(time.Millisecond)
	}

	return h, cancel, func() {
		cancel()
		for _, c := range conns {
			c.Close()
		}
		serverCleanup()
	}
}

// ============================================================================
// Hub Benchmarks
// ============================================================================

// BenchmarkHub_Broadcast measures hub broadcast throughput with varying client counts.
func BenchmarkHub_Broadcast(b *testing.B) {
	for _, numClients := range []int{1, 10, 50, 100} {
		b.Run(fmt.Sprintf("clients_%d", numClients), func(b *testing.B) {
			h, _, cleanup := benchHub(b, numClients, 4096)
			defer cleanup()

			msg := []byte(`{"level":"info","message":"benchmark log message","timestamp":"2024-01-15T10:30:45Z"}`)

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				h.Broadcast(msg)
			}
		})
	}
}

// BenchmarkHub_BroadcastMessageSizes measures broadcast performance at different message sizes.
func BenchmarkHub_BroadcastMessageSizes(b *testing.B) {
	for _, size := range []int{64, 256, 1024, 4096, 16384} {
		b.Run(fmt.Sprintf("bytes_%d", size), func(b *testing.B) {
			h, _, cleanup := benchHub(b, 10, 4096)
			defer cleanup()

			msg := make([]byte, size)
			for i := range msg {
				msg[i] = 'A' + byte(i%26)
			}

			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()

			for b.Loop() {
				h.Broadcast(msg)
			}
		})
	}
}

// ============================================================================
// Parse Benchmarks
// ============================================================================

// BenchmarkNormalizeContainerLine measures log line parsing performance.
func BenchmarkNormalizeContainerLine(b *testing.B) {
	cases := map[string]string{
		"plain":              "hello world this is a normal log message",
		"stderr_prefix":      "[STDERR] error occurred in module X",
		"with_timestamp":     "2024-01-15T10:30:45.123456789Z application started successfully on port 8080",
		"stderr_timestamp":   "[STDERR] 2024-06-01T12:00:00.000Z critical error in database connection",
		"long_line":          strings.Repeat("abcdefghij", 100),
		"with_trailing_crlf": "message with trailing chars\r\n",
	}

	for name, input := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				NormalizeContainerLine(input)
			}
		})
	}
}

// BenchmarkNormalizeProjectLine measures project log line parsing performance.
func BenchmarkNormalizeProjectLine(b *testing.B) {
	cases := map[string]string{
		"with_service":    "web | Starting server on port 8080",
		"without_service": "plain log line without service",
		"complex":         "2024-01-15T10:30:45.123Z api-gateway | Request received from 192.168.1.1",
	}

	for name, input := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				NormalizeProjectLine(input)
			}
		})
	}
}

// ============================================================================
// JSON Marshaling Benchmarks
// ============================================================================

// BenchmarkLogMessage_Marshal measures JSON marshaling cost for a single log message.
func BenchmarkLogMessage_Marshal(b *testing.B) {
	msg := LogMessage{
		Seq:         42,
		Level:       "stdout",
		Message:     "2024-01-15 10:30:45.123 INFO  [main] Application started successfully on port 8080",
		Timestamp:   "2024-01-15T10:30:45.123456789Z",
		Service:     "web-api",
		ContainerID: "abc123def456",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := json.Marshal(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLogMessageBatch_Marshal measures batch JSON marshaling cost.
func BenchmarkLogMessageBatch_Marshal(b *testing.B) {
	for _, batchSize := range []int{1, 10, 50, 100} {
		b.Run(fmt.Sprintf("batch_%d", batchSize), func(b *testing.B) {
			batch := make([]LogMessage, batchSize)
			for i := range batch {
				batch[i] = LogMessage{
					Seq:       uint64(i), //nolint:gosec // range index is non-negative
					Level:     "stdout",
					Message:   fmt.Sprintf("log message number %d from service", i),
					Timestamp: "2024-01-15T10:30:45.123456789Z",
					Service:   "web-api",
				}
			}

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				if _, err := json.Marshal(batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// ============================================================================
// Bridge Throughput Benchmarks
// ============================================================================

// BenchmarkForwardLogJSONBatched_Throughput measures end-to-end batched forwarding throughput.
func BenchmarkForwardLogJSONBatched_Throughput(b *testing.B) {
	for _, batchSize := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("maxBatch_%d", batchSize), func(b *testing.B) {
			h, _, cleanup := benchHub(b, 1, 8192)
			defer cleanup()

			b.ReportAllocs()
			b.ResetTimer()

			ctx := context.Background()
			logs := make(chan LogMessage, b.N)
			for range b.N {
				logs <- LogMessage{
					Seq:       1,
					Level:     "stdout",
					Message:   "benchmark log message for throughput testing",
					Timestamp: "2024-01-15T10:30:45.123456789Z",
					Service:   "api",
				}
			}
			close(logs)

			ForwardLogJSONBatched(ctx, h, logs, batchSize, 10*time.Millisecond)
		})
	}
}

// BenchmarkForwardLines_Throughput measures plain text line forwarding throughput.
func BenchmarkForwardLines_Throughput(b *testing.B) {
	h, _, cleanup := benchHub(b, 1, 8192)
	defer cleanup()

	b.ReportAllocs()
	b.ResetTimer()

	ctx := context.Background()
	lines := make(chan string, b.N)
	for range b.N {
		lines <- "2024-01-15T10:30:45Z this is a benchmark log line for testing throughput performance"
	}
	close(lines)

	ForwardLines(ctx, h, lines)
}

// ============================================================================
// Memory Benchmarks
// ============================================================================

// BenchmarkHub_MemoryPerClient measures memory overhead per connected client.
func BenchmarkHub_MemoryPerClient(b *testing.B) {
	for _, sendBuf := range []int{16, 64, 256} {
		b.Run(fmt.Sprintf("sendBuffer_%d", sendBuf), func(b *testing.B) {
			h := NewHub(100)
			ctx := b.Context()
			go h.Run(ctx)

			wsURL, serverCleanup := benchWSServer(b)
			defer serverCleanup()

			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
				if err != nil {
					b.Fatal(err)
				}
				if resp != nil {
					resp.Body.Close()
				}
				c := NewClient(conn, sendBuf)
				h.register <- c
				go func() {
					for range c.send {
					}
				}()
			}
		})
	}
}

// BenchmarkNowRFC3339 measures timestamp generation performance.
func BenchmarkNowRFC3339(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		NowRFC3339()
	}
}
