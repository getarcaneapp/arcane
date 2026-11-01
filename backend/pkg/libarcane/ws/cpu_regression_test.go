package ws

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// startStatsHubPipeline starts the stats producer and JSON broadcaster goroutines
// for benchmarks/tests that need a container-stats-style hub. Caller must run hub.Run and ServeClient.
func startStatsHubPipeline(ctx context.Context, hub *Hub) {
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
				case statsChan <- map[string]float64{"cpu": 10.0, "memory": 1024.0}:
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
				if data, err := json.Marshal(stats); err == nil {
					hub.Broadcast(data)
				}
			}
		}
	}()
}

// readOneWithTimeout reads a single message with a timeout, discarding the payload.
func readOneWithTimeout(tb testing.TB, conn *websocket.Conn) error {
	readCtx, cancel := context.WithTimeout(tb.Context(), 2*time.Second)
	defer cancel()
	_, _, err := conn.Read(readCtx)
	return err
}

// ============================================================================
// CPU Regression Benchmarks
//
// These benchmarks simulate the exact usage patterns from the bug report:
// https://github.com/getarcaneapp/arcane/issues/XXXX
//
// The bug: CPU load climbs permanently after using Arcane, especially after
// reloading the container page 20-30 times. Root cause is goroutine leaks
// from Hub+streaming pipelines not being properly cleaned up.
//
// These benchmarks measure:
// 1. Goroutine count growth over repeated connect/disconnect cycles
// 2. Per-cycle overhead to detect if cleanup is happening
// 3. Memory growth to catch leaked resources
// ============================================================================

// BenchmarkCPU_PageReloadSimulation simulates the exact bug scenario:
// repeatedly connecting and disconnecting to a container stats WebSocket
// (like reloading the container page). Each connection creates a full pipeline.
//
// Run with: go test -bench=BenchmarkCPU_PageReloadSimulation -benchtime=30x -benchmem
//
// A healthy result shows constant goroutine count and stable allocations.
// A regression shows goroutine count growing linearly with iterations.
func BenchmarkCPU_PageReloadSimulation(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		hub := NewHub(64)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx)
		startStatsHubPipeline(ctx, hub)
		ServeClientWithOnRemove(ctx, hub, conn, nil)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		conn, _, err := websocket.Dial(b.Context(), url, nil)
		if err != nil {
			require.FailNowf(b, "benchmark websocket dial failed", "%v", err)
		}

		// Read one message to exercise the pipeline
		_ = readOneWithTimeout(b, conn)
		_ = conn.CloseNow()

		// Brief settle for cleanup
		time.Sleep(50 * time.Millisecond)

		// Report goroutine growth periodically
		if (i+1)%10 == 0 || i == b.N-1 {
			current := runtime.NumGoroutine()
			b.ReportMetric(float64(current-baselineGoroutines), "goroutine_delta")
		}
	}

	b.StopTimer()

	// Final goroutine check
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	b.ReportMetric(float64(finalGoroutines-baselineGoroutines), "final_goroutine_leak")
}

// BenchmarkCPU_ContainerLogReloadSimulation simulates reloading a container
// logs page with JSON batched output (the most complex pipeline).
func BenchmarkCPU_ContainerLogReloadSimulation(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		hub := NewHub(1024)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx)

		lines := make(chan string, 256)
		go func() {
			defer close(lines)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					lines <- "2024-01-15T10:30:45.123Z INFO container log message"
				}
			}
		}()

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
				msgs <- LogMessage{Seq: seq, Level: level, Message: msg, Timestamp: ts}
			}
		}()

		go ForwardLogJSONBatched(ctx, hub, msgs, 50, 400*time.Millisecond)

		ServeClientWithOnRemove(ctx, hub, conn, nil)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baselineGoroutines := runtime.NumGoroutine()

	b.ReportAllocs()
	b.ResetTimer()

	for i := range b.N {
		conn, _, err := websocket.Dial(b.Context(), url, nil)
		if err != nil {
			require.FailNowf(b, "benchmark websocket dial failed", "%v", err)
		}

		_ = readOneWithTimeout(b, conn)
		_ = conn.CloseNow()
		time.Sleep(50 * time.Millisecond)
		if (i+1)%10 == 0 || i == b.N-1 {
			current := runtime.NumGoroutine()
			b.ReportMetric(float64(current-baselineGoroutines), "goroutine_delta")
		}
	}
	b.StopTimer()
	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	b.ReportMetric(float64(finalGoroutines-baselineGoroutines), "final_goroutine_leak")
}

// BenchmarkCPU_GoroutineScaling measures how goroutine count scales with
// concurrent connections. This catches leaks that only manifest under load.
func BenchmarkCPU_GoroutineScaling(b *testing.B) {
	for _, concurrency := range []int{1, 5, 10, 20} {
		b.Run(fmt.Sprintf("concurrent_%d", concurrency), func(b *testing.B) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				ctx, cancel := context.WithCancel(r.Context())
				hub := NewHub(64)
				hub.SetOnEmpty(func() { cancel() })
				go hub.Run(ctx)
				startStatsHubPipeline(ctx, hub)
				ServeClientWithOnRemove(ctx, hub, conn, nil)
			}))
			defer server.Close()

			url := "ws" + strings.TrimPrefix(server.URL, "http")

			runtime.GC()
			time.Sleep(100 * time.Millisecond)
			baselineGoroutines := runtime.NumGoroutine()

			b.ReportAllocs()
			b.ResetTimer()

			for range b.N {
				// Open `concurrency` connections simultaneously
				conns := make([]*websocket.Conn, concurrency)
				for j := range concurrency {
					conn, _, err := websocket.Dial(b.Context(), url, nil)
					if err != nil {
						require.FailNowf(b, "benchmark websocket dial failed", "%v", err)
					}
					conns[j] = conn
				}

				for _, c := range conns {
					_ = readOneWithTimeout(b, c)
				}

				// Close all
				for _, conn := range conns {
					_ = conn.CloseNow()
				}

				time.Sleep(100 * time.Millisecond)
			}

			b.StopTimer()
			time.Sleep(1 * time.Second)
			runtime.GC()
			time.Sleep(200 * time.Millisecond)
			finalGoroutines := runtime.NumGoroutine()
			b.ReportMetric(float64(finalGoroutines-baselineGoroutines), "final_goroutine_leak")
		})
	}
}

// BenchmarkCPU_SustainedStreaming measures CPU overhead of maintaining
// an active streaming connection over time. This catches CPU drift from
// inefficient polling or tight loops.
func BenchmarkCPU_SustainedStreaming(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		hub := NewHub(1024)
		hub.SetOnEmpty(func() { cancel() })
		go hub.Run(ctx)
		startStatsHubPipeline(ctx, hub)
		ServeClientWithOnRemove(ctx, hub, conn, nil)
	}))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		conn, _, err := websocket.Dial(b.Context(), url, nil)
		if err != nil {
			require.FailNowf(b, "benchmark websocket dial failed", "%v", err)
		}
		for range 10 {
			if readOneWithTimeoutErr := readOneWithTimeout(b, conn); readOneWithTimeoutErr != nil {
				break
			}
		}
		_ = conn.CloseNow()
		time.Sleep(50 * time.Millisecond)
	}
}
