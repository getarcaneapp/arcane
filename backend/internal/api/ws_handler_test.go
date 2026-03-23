package api

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

	docker "github.com/getarcaneapp/arcane/backend/pkg/dockerutil"
	ws "github.com/getarcaneapp/arcane/backend/pkg/libarcane/ws"
	systemtypes "github.com/getarcaneapp/arcane/types/system"
	"github.com/gin-gonic/gin"
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

	handler := newTestWebSocketHandler()
	handler.broadcastContainerLogStreamErrorInternal("container-1", "json", errors.New("boom"), stream)

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

	handler := newTestWebSocketHandler()
	handler.broadcastContainerLogStreamErrorInternal("container-1", "text", errors.New("boom"), stream)

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

func newTestWebSocketHandler() *WebSocketHandler {
	return &WebSocketHandler{
		wsMetrics:  NewWebSocketMetrics(),
		logStreams: make(map[string]*wsLogStream),
		wsUpgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func dialWebSocket(t *testing.T, serverURL, path string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + path
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}

	return conn
}

func TestWebSocketHandler_ProjectLogs_SharedStreamPerTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

	router := gin.New()
	router.GET("/api/environments/:id/ws/projects/:projectId/logs", handler.ProjectLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	conn1 := dialWebSocket(t, server.URL, "/api/environments/0/ws/projects/project-1/logs")
	conn2 := dialWebSocket(t, server.URL, "/api/environments/0/ws/projects/project-1/logs")

	_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn1.ReadMessage()
	require.NoError(t, err)

	_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn2.ReadMessage()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return starts.Load() == 1
	}, 2*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		handler.logStreamsMu.Lock()
		defer handler.logStreamsMu.Unlock()
		return len(handler.logStreams) == 1
	}, time.Second, 20*time.Millisecond)

	require.NoError(t, conn1.Close())

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

	require.NoError(t, conn2.Close())

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
	gin.SetMode(gin.TestMode)

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

	router := gin.New()
	router.GET("/api/environments/:id/ws/projects/:projectId/logs", handler.ProjectLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/environments/0/ws/projects/project-1/logs?follow=false"
	conn1 := dialWebSocket(t, server.URL, path)
	defer conn1.Close()

	_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg1, err := conn1.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "finite project log", string(msg1))

	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first finite log stream did not complete")
	}

	conn2 := dialWebSocket(t, server.URL, path)
	defer conn2.Close()

	_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg2, err := conn2.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "finite project log", string(msg2))

	require.Eventually(t, func() bool {
		return starts.Load() == 2
	}, 2*time.Second, 20*time.Millisecond)
}

func TestWebSocketHandler_ContainerLogs_BroadcastsStreamErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := newTestWebSocketHandler()
	handler.containerLogStreamer = func(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case logsChan <- "api | container log":
		}
		return errors.New("stream failed")
	}

	router := gin.New()
	router.GET("/api/environments/:id/ws/containers/:containerId/logs", handler.ContainerLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	conn := dialWebSocket(t, server.URL, "/api/environments/0/ws/containers/container-1/logs")
	defer conn.Close()

	var got []string

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	got = append(got, string(msg))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err = conn.ReadMessage()
	require.NoError(t, err)
	got = append(got, string(msg))

	require.ElementsMatch(t, []string{
		"api | container log",
		"Failed to stream container logs: stream failed",
	}, got)
}

func TestWebSocketHandler_ContainerLogs_ErrorStartsFreshStreamForNewSubscribers(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

	router := gin.New()
	router.GET("/api/environments/:id/ws/containers/:containerId/logs", handler.ContainerLogs)
	server := httptest.NewServer(router)
	defer server.Close()

	path := "/api/environments/0/ws/containers/container-1/logs"
	conn1 := dialWebSocket(t, server.URL, path)
	defer conn1.Close()

	got1 := make([]string, 0, 2)
	for range 2 {
		_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn1.ReadMessage()
		require.NoError(t, err)
		got1 = append(got1, string(msg))
	}
	require.ElementsMatch(t, []string{
		"api | container log",
		"Failed to stream container logs: stream failed",
	}, got1)

	conn2 := dialWebSocket(t, server.URL, path)
	defer conn2.Close()

	got2 := make([]string, 0, 2)
	for range 2 {
		_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := conn2.ReadMessage()
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
	gin.SetMode(gin.TestMode)

	handler := newTestWebSocketHandler()
	var collects atomic.Int32

	handler.systemStatsCollector = func(ctx context.Context) systemtypes.SystemStats {
		n := collects.Add(1)
		return systemtypes.SystemStats{
			CPUUsage: float64(n),
		}
	}

	router := gin.New()
	router.GET("/api/environments/:id/ws/system/stats", handler.SystemStats)
	server := httptest.NewServer(router)
	defer server.Close()

	conn1 := dialWebSocket(t, server.URL, "/api/environments/0/ws/system/stats?interval=1")
	conn2 := dialWebSocket(t, server.URL, "/api/environments/0/ws/system/stats?interval=1")

	_ = conn1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err := conn1.ReadMessage()
	require.NoError(t, err)

	_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _, err = conn2.ReadMessage()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return collects.Load() >= 1
	}, 2*time.Second, 50*time.Millisecond)

	require.NoError(t, conn1.Close())
	require.NoError(t, conn2.Close())

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
		t.Fatal("first sampler acquisition did not finish")
	}

	select {
	case <-secondDone:
	case <-time.After(2 * time.Second):
		t.Fatal("second sampler acquisition did not wait for readiness")
	}

	stats := handler.latestSystemStatsSnapshotInternal()
	require.Equal(t, 42.0, stats.CPUUsage)

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
		t.Fatal("sampler did not start CPU initialization")
	}

	cancel()

	select {
	case ok := <-done:
		require.False(t, ok)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sampler acquisition did not stop waiting after cancellation")
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
		hub:    ws.NewHub(1),
		cancel: func() { staleCancels.Add(1) },
		refs:   2,
		done:   true,
	}
	handler.logStreams[key] = stale

	var freshCancels atomic.Int32
	fresh := handler.getOrCreateLogStreamInternal(key, func(onEmpty func(*wsLogStream)) *wsLogStream {
		return &wsLogStream{
			hub:    ws.NewHub(1),
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
		hub:    ws.NewHub(1),
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
	handler.cgroupCache.timestamp = time.Now().Add(-2 * cgroupCacheTTL)

	var calls atomic.Int32
	start := make(chan struct{})
	release := make(chan struct{})
	handler.cgroupLimitsDetector = func() (*docker.CgroupLimits, error) {
		calls.Add(1)
		close(start)
		<-release
		return &docker.CgroupLimits{CPUCount: 2}, nil
	}

	const goroutines = 8
	results := make(chan *docker.CgroupLimits, goroutines)
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
		t.Fatal("detector was not called")
	}

	require.Equal(t, int32(1), calls.Load())

	close(release)
	wg.Wait()
	close(results)

	for result := range results {
		require.NotNil(t, result)
		require.Equal(t, 2, result.CPUCount)
	}
	require.Equal(t, int32(1), calls.Load())
}
