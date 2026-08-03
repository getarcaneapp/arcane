package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/system"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/sys/cgroup"
)

func TestBroadcastLogStreamErrorInternal_JSON(t *testing.T) {
	hub := wshub.NewHub(10)
	ctx := t.Context()
	go hub.Run(ctx)

	clientConn, serverConn, cleanup := newTestWSPairInternal(t)
	t.Cleanup(cleanup)

	wshub.ServeClientWithOnRemove(ctx, hub, serverConn, nil)

	stream := &wsLogStream{
		hub:    hub,
		format: "json",
	}

	broadcastLogStreamErrorInternal("container log stream", "Failed to stream container logs: ", "container-1", "json", errors.New("boom"), stream)

	payload, err := readMessageInternal(t, clientConn)
	require.NoError(t, err)

	var message wshub.LogMessage
	require.NoError(t, json.Unmarshal(payload, &message))
	require.Equal(t, "error", message.Level)
	require.Equal(t, "Failed to stream container logs: boom", message.Message)
	require.Empty(t, message.ContainerID)
	require.NotEmpty(t, message.Timestamp)
}

func TestBroadcastLogStreamErrorInternal_Text(t *testing.T) {
	hub := wshub.NewHub(10)
	ctx := t.Context()
	go hub.Run(ctx)

	clientConn, serverConn, cleanup := newTestWSPairInternal(t)
	t.Cleanup(cleanup)

	wshub.ServeClientWithOnRemove(ctx, hub, serverConn, nil)

	stream := &wsLogStream{
		hub:    hub,
		format: "text",
	}

	broadcastLogStreamErrorInternal("container log stream", "Failed to stream container logs: ", "container-1", "text", errors.New("boom"), stream)

	payload, err := readMessageInternal(t, clientConn)
	require.NoError(t, err)
	require.Equal(t, "Failed to stream container logs: boom", string(payload))
}

func TestExecHandleControlMessageInternal_Resize(t *testing.T) {
	var gotCols, gotRows uint
	resize := func(cols, rows uint) { gotCols, gotRows = cols, rows }

	handled := execHandleControlMessageInternal([]byte(`{"type":"resize","cols":80,"rows":24}`), resize)

	require.True(t, handled)
	require.EqualValues(t, 80, gotCols)
	require.EqualValues(t, 24, gotRows)
}

func TestExecHandleControlMessageInternal_ResizeOutOfBoundsIsIgnored(t *testing.T) {
	called := false
	resize := func(uint, uint) { called = true }

	for _, payload := range []string{
		`{"type":"resize","cols":0,"rows":24}`,
		`{"type":"resize","cols":80,"rows":0}`,
		`{"type":"resize","cols":1001,"rows":24}`,
		`{"type":"resize","cols":80,"rows":1001}`,
	} {
		handled := execHandleControlMessageInternal([]byte(payload), resize)
		require.True(t, handled, payload)
	}
	require.False(t, called, "resize must not be invoked for out-of-bounds dimensions")
}

func TestExecHandleControlMessageInternal_UnknownTypeIsConsumedNotWrittenToStdin(t *testing.T) {
	resize := func(uint, uint) { t.Fatal("resize must not be called for a non-resize message") }

	handled := execHandleControlMessageInternal([]byte(`{"type":"bogus"}`), resize)

	require.True(t, handled, "a parseable JSON control frame must always be consumed, even with an unknown type")
}

func TestExecHandleControlMessageInternal_MalformedFallsThroughToStdin(t *testing.T) {
	resize := func(uint, uint) { t.Fatal("resize must not be called for malformed input") }

	for _, payload := range []string{`{"garbage`, `not json at all`, ``} {
		handled := execHandleControlMessageInternal([]byte(payload), resize)
		require.False(t, handled, payload)
	}
}

func TestExecHandleControlMessageInternal_OversizedPayloadFallsThroughToStdin(t *testing.T) {
	resize := func(uint, uint) { t.Fatal("resize must not be called for an oversized payload") }

	oversized := `{"type":"resize","cols":80,"rows":24,"padding":"` + strings.Repeat("a", execControlMaxBytes) + `"}`
	handled := execHandleControlMessageInternal([]byte(oversized), resize)

	require.False(t, handled)
}

func TestExecHandleControlMessageInternal_NilResizeCallbackIsSafe(t *testing.T) {
	handled := execHandleControlMessageInternal([]byte(`{"type":"resize","cols":80,"rows":24}`), nil)
	require.True(t, handled)
}

func newTestWSPairInternal(t *testing.T) (clientConn *websocket.Conn, serverConn *websocket.Conn, cleanup func()) {
	t.Helper()
	serverReady := make(chan *websocket.Conn, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		serverReady <- conn
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)

	serverConn = <-serverReady

	return clientConn, serverConn, func() {
		_ = clientConn.CloseNow()
		_ = serverConn.CloseNow()
		server.Close()
	}
}

func newTestWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		wsMetrics:     wshub.NewWebSocketMetrics(),
		logStreams:    make(map[string]*wsLogStream),
		cgroupCache:   cgroup.NewCache(cgroupCacheTTL),
		gpuMonitor:    system.NewGPUMonitor(false, ""),
		checkWSOrigin: func(*http.Request) bool { return true },
	}
}

func dialWebSocket(t *testing.T, serverURL, path string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + path
	conn, _, err := websocket.Dial(t.Context(), wsURL, nil)
	require.NoError(t, err)

	return conn
}

func readMessageInternal(t *testing.T, conn *websocket.Conn) ([]byte, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	return payload, err
}

func TestWebSocketHandler_ProjectLogs_SharedStreamPerTarget(t *testing.T) {

	handler := newTestWebSocketHandler()
	var starts atomic.Int32
	var cancels atomic.Int32

	handler.projectLogStreamer = func(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
		starts.Add(1)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		defer cancels.Add(1)

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				select {
				case <-ctx.Done():
					return ctx.Err()
				case logsChan <- "api | shared project log":
				}
			}
		}
	}

	router := echo.New()
	router.GET("/api/environments/:id/ws/projects/:projectId/logs", handler.ProjectLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	conn1 := dialWebSocket(t, server.URL, "/api/environments/0/ws/projects/project-1/logs")
	conn2 := dialWebSocket(t, server.URL, "/api/environments/0/ws/projects/project-1/logs")

	_, err := readMessageInternal(t, conn1)
	require.NoError(t, err)

	_, err = readMessageInternal(t, conn2)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return starts.Load() == 1
	}, 2*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		handler.logStreamsMu.Lock()
		defer handler.logStreamsMu.Unlock()
		return len(handler.logStreams) == 1
	}, time.Second, 20*time.Millisecond)

	require.NoError(t, conn1.CloseNow())

	require.Eventually(t, func() bool {
		handler.logStreamsMu.Lock()
		defer handler.logStreamsMu.Unlock()
		return len(handler.logStreams) == 1
	}, 2*time.Second, 10*time.Millisecond)

	handler.logStreamsMu.Lock()
	activeAfterFirstClose := len(handler.logStreams)
	handler.logStreamsMu.Unlock()
	require.Equal(t, 1, activeAfterFirstClose)
	require.Equal(t, int32(0), cancels.Load())

	require.NoError(t, conn2.CloseNow())

	require.Eventually(t, func() bool {
		handler.logStreamsMu.Lock()
		defer handler.logStreamsMu.Unlock()
		return len(handler.logStreams) == 0
	}, 2*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		return cancels.Load() == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestWebSocketHandler_ProjectLogs_CompletedSourceStartsFreshStream(t *testing.T) {

	handler := newTestWebSocketHandler()
	var starts atomic.Int32
	firstDone := make(chan struct{})

	handler.projectLogStreamer = func(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
		call := starts.Add(1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case logsChan <- "api | finite project log":
		}
		if call == 1 {
			close(firstDone)
		}
		return nil
	}

	router := echo.New()
	router.GET("/api/environments/:id/ws/projects/:projectId/logs", handler.ProjectLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/environments/0/ws/projects/project-1/logs?follow=false"
	conn1 := dialWebSocket(t, server.URL, path)
	defer func() {
		_ = conn1.CloseNow()
	}()

	msg1, err := readMessageInternal(t, conn1)
	require.NoError(t, err)
	require.Equal(t, "finite project log", string(msg1))

	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first finite log stream did not complete")
	}

	conn2 := dialWebSocket(t, server.URL, path)
	defer func() {
		_ = conn2.CloseNow()
	}()

	msg2, err := readMessageInternal(t, conn2)
	require.NoError(t, err)
	require.Equal(t, "finite project log", string(msg2))

	require.Eventually(t, func() bool {
		return starts.Load() == 2
	}, 2*time.Second, 20*time.Millisecond)
}

func TestWebSocketHandler_ContainerLogs_BroadcastsStreamErrors(t *testing.T) {

	handler := newTestWebSocketHandler()
	handler.containerLogStreamer = func(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case logsChan <- "api | container log":
		}
		return errors.New("stream failed")
	}

	router := echo.New()
	router.GET("/api/environments/:id/ws/containers/:containerId/logs", handler.ContainerLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	conn := dialWebSocket(t, server.URL, "/api/environments/0/ws/containers/container-1/logs")
	defer func() {
		_ = conn.CloseNow()
	}()

	var got []string

	msg, err := readMessageInternal(t, conn)
	require.NoError(t, err)
	got = append(got, string(msg))

	msg, err = readMessageInternal(t, conn)
	require.NoError(t, err)
	got = append(got, string(msg))

	require.ElementsMatch(t, []string{
		"api | container log",
		"Failed to stream container logs: stream failed",
	}, got)
}

func TestWebSocketHandler_ContainerLogs_ErrorStartsFreshStreamForNewSubscribers(t *testing.T) {

	handler := newTestWebSocketHandler()
	var starts atomic.Int32
	handler.containerLogStreamer = func(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
		starts.Add(1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case logsChan <- "api | container log":
		}
		return errors.New("stream failed")
	}

	router := echo.New()
	router.GET("/api/environments/:id/ws/containers/:containerId/logs", handler.ContainerLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/environments/0/ws/containers/container-1/logs"
	conn1 := dialWebSocket(t, server.URL, path)
	defer func() {
		_ = conn1.CloseNow()
	}()

	got1 := make([]string, 0, 2)
	for range 2 {
		msg, err := readMessageInternal(t, conn1)
		require.NoError(t, err)
		got1 = append(got1, string(msg))
	}
	require.ElementsMatch(t, []string{
		"api | container log",
		"Failed to stream container logs: stream failed",
	}, got1)

	conn2 := dialWebSocket(t, server.URL, path)
	defer func() {
		_ = conn2.CloseNow()
	}()

	got2 := make([]string, 0, 2)
	for range 2 {
		msg, err := readMessageInternal(t, conn2)
		require.NoError(t, err)
		got2 = append(got2, string(msg))
	}
	require.ElementsMatch(t, []string{
		"api | container log",
		"Failed to stream container logs: stream failed",
	}, got2)

	require.Eventually(t, func() bool {
		return starts.Load() == 2
	}, 2*time.Second, 20*time.Millisecond)
}

func TestWebSocketHandler_SystemStats_UsesSharedSampler(t *testing.T) {

	handler := newTestWebSocketHandler()
	var collects atomic.Int32

	handler.systemStatsCollector = func(ctx context.Context) systemtypes.SystemStats {
		n := collects.Add(1)
		return systemtypes.SystemStats{
			CPUUsage: float64(n),
		}
	}

	router := echo.New()
	router.GET("/api/environments/:id/ws/system/stats", handler.SystemStats)
	server := httptest.NewServer(router)
	defer server.Close()

	conn1 := dialWebSocket(t, server.URL, "/api/environments/0/ws/system/stats?interval=1")
	conn2 := dialWebSocket(t, server.URL, "/api/environments/0/ws/system/stats?interval=1")

	_, err := readMessageInternal(t, conn1)
	require.NoError(t, err)

	_, err = readMessageInternal(t, conn2)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return collects.Load() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	require.NoError(t, conn1.CloseNow())
	require.NoError(t, conn2.CloseNow())

	require.Eventually(t, func() bool {
		handler.systemStatsSampler.lifecycleMu.Lock()
		defer handler.systemStatsSampler.lifecycleMu.Unlock()
		return !handler.systemStatsSampler.running && handler.systemStatsSampler.clients == 0
	}, 2*time.Second, 20*time.Millisecond)

	stoppedAt := collects.Load()
	require.Never(t, func() bool {
		return collects.Load() != stoppedAt
	}, 1200*time.Millisecond, 100*time.Millisecond)
}

func TestWebSocketHandler_AcquireSystemStatsSampler_WaitsForInitialSnapshot(t *testing.T) {
	handler := newTestWebSocketHandler()
	handler.systemStatsCollector = func(ctx context.Context) systemtypes.SystemStats {
		return systemtypes.SystemStats{CPUUsage: 42}
	}

	firstDone := make(chan struct{})
	go func() {
		handler.acquireSystemStatsSamplerInternal(context.Background())
		close(firstDone)
	}()

	require.Eventually(t, func() bool {
		handler.systemStatsSampler.lifecycleMu.Lock()
		defer handler.systemStatsSampler.lifecycleMu.Unlock()
		return handler.systemStatsSampler.running && handler.systemStatsSampler.ready != nil
	}, 500*time.Millisecond, 10*time.Millisecond)

	secondDone := make(chan struct{})
	go func() {
		handler.acquireSystemStatsSamplerInternal(context.Background())
		close(secondDone)
	}()

	require.Never(t, func() bool {
		select {
		case <-secondDone:
			return true
		default:
			return false
		}
	}, 200*time.Millisecond, 20*time.Millisecond)

	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "first sampler acquisition did not finish")
	}

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "second sampler acquisition did not wait for readiness")
	}

	stats := handler.latestSystemStatsSnapshotInternal()
	require.InDelta(t, 42.0, stats.CPUUsage, 0.000001)

	handler.releaseSystemStatsSamplerInternal()
	handler.releaseSystemStatsSamplerInternal()

	require.Eventually(t, func() bool {
		handler.systemStatsSampler.lifecycleMu.Lock()
		defer handler.systemStatsSampler.lifecycleMu.Unlock()
		return !handler.systemStatsSampler.running && handler.systemStatsSampler.clients == 0 && handler.systemStatsSampler.ready == nil
	}, 2*time.Second, 20*time.Millisecond)
}

func TestWebSocketHandler_AcquireSystemStatsSampler_StopsWaitingWhenCallerCancels(t *testing.T) {
	handler := newTestWebSocketHandler()

	readyToCancel := make(chan struct{})
	handler.systemStatsCollector = func(ctx context.Context) systemtypes.SystemStats {
		return systemtypes.SystemStats{CPUUsage: 42}
	}

	handler.cpuUsageReader = func(interval time.Duration) (float64, bool) {
		close(readyToCancel)
		time.Sleep(200 * time.Millisecond)
		return 42, true
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- handler.acquireSystemStatsSamplerInternal(ctx)
	}()

	select {
	case <-readyToCancel:
	case <-time.After(time.Second):
		require.FailNow(t, "sampler did not start CPU initialization")
	}

	cancel()

	select {
	case ok := <-done:
		require.False(t, ok)
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "sampler acquisition did not stop waiting after cancellation")
	}

	handler.releaseSystemStatsSamplerInternal()

	require.Eventually(t, func() bool {
		handler.systemStatsSampler.lifecycleMu.Lock()
		defer handler.systemStatsSampler.lifecycleMu.Unlock()
		return !handler.systemStatsSampler.running && handler.systemStatsSampler.clients == 0
	}, time.Second, 20*time.Millisecond)
}

func TestWebSocketHandler_LogStream_ReplacesDoneStreamAndCleansStaleRefs(t *testing.T) {
	handler := newTestWebSocketHandler()
	key := "env|project|resource|text|false|true|100||false"

	var staleCancels atomic.Int32
	stale := &wsLogStream{
		hub:    wshub.NewHub(1),
		cancel: func() { staleCancels.Add(1) },
		refs:   2,
		done:   true,
	}
	handler.logStreams[key] = stale

	var freshCancels atomic.Int32
	fresh := handler.getOrCreateLogStreamInternal(key, func(onEmpty func(*wsLogStream)) *wsLogStream {
		return &wsLogStream{
			hub:    wshub.NewHub(1),
			cancel: func() { freshCancels.Add(1) },
		}
	})

	require.NotSame(t, stale, fresh)
	handler.logStreamsMu.Lock()
	require.Same(t, fresh, handler.logStreams[key])
	handler.logStreamsMu.Unlock()

	handler.markLogStreamDoneInternal(key, stale)
	handler.logStreamsMu.Lock()
	require.Same(t, fresh, handler.logStreams[key])
	handler.logStreamsMu.Unlock()

	handler.releaseLogStreamInternal(key, stale)
	require.Equal(t, 1, stale.refs)
	handler.logStreamsMu.Lock()
	require.Same(t, fresh, handler.logStreams[key])
	handler.logStreamsMu.Unlock()

	handler.releaseLogStreamInternal(key, stale)
	require.Equal(t, int32(1), staleCancels.Load())
	handler.logStreamsMu.Lock()
	require.Same(t, fresh, handler.logStreams[key])
	handler.logStreamsMu.Unlock()

	handler.releaseLogStreamInternal(key, fresh)
	require.Equal(t, int32(1), freshCancels.Load())
	handler.logStreamsMu.Lock()
	_, ok := handler.logStreams[key]
	handler.logStreamsMu.Unlock()
	require.False(t, ok)
}

func TestWebSocketHandler_LogStream_CancelsOnlyOnce(t *testing.T) {
	handler := newTestWebSocketHandler()
	key := "env|project|resource|text|false|true|100||false"

	var cancels atomic.Int32
	stream := &wsLogStream{
		hub:    wshub.NewHub(1),
		cancel: func() { cancels.Add(1) },
		refs:   1,
	}
	handler.logStreams[key] = stream

	handler.markLogStreamDoneInternal(key, stream)
	handler.releaseLogStreamInternal(key, stream)
	handler.markLogStreamDoneInternal(key, stream)

	require.Equal(t, int32(1), cancels.Load())
}

func TestWebSocketHandler_GetCachedCgroupLimitsInternal_DeduplicatesRefresh(t *testing.T) {
	handler := newTestWebSocketHandler()

	var calls atomic.Int32
	start := make(chan struct{})
	var startOnce sync.Once
	release := make(chan struct{})
	// A fresh cache has a zero timestamp, so the first Get takes the refresh path
	// and subsequent concurrent Gets dedupe behind the write lock. A goroutine
	// can miss both the cached value and the in-flight refresh at the flight
	// boundary and run the detector a second time after the first completes, so
	// close(start) must be idempotent.
	handler.cgroupCache = cgroup.NewCacheWithDetector(cgroupCacheTTL, func() (*cgroup.Limits, error) {
		calls.Add(1)
		startOnce.Do(func() { close(start) })
		<-release
		return &cgroup.Limits{CPUCount: 2}, nil
	})

	const goroutines = 8
	results := make(chan *cgroup.Limits, goroutines)
	ready := make(chan struct{})
	var entered sync.WaitGroup
	entered.Add(goroutines)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			<-ready
			entered.Done()
			results <- handler.getCachedCgroupLimitsInternal()
		})
	}

	close(ready)
	entered.Wait()

	select {
	case <-start:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "detector was not called")
	}

	require.Equal(t, int32(1), calls.Load())

	close(release)
	wg.Wait()
	close(results)

	for result := range results {
		require.NotNil(t, result)
		require.Equal(t, 2, result.CPUCount)
	}
	// Dedup guarantees no concurrent detector calls (asserted above while the
	// refresh was in flight); a single boundary straggler may legitimately
	// trigger one extra refresh after the first flight completes.
	require.LessOrEqual(t, calls.Load(), int32(2))
}
