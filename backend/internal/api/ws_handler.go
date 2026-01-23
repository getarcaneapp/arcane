package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/common"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils/docker"
	httputil "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	ws "github.com/getarcaneapp/arcane/backend/internal/utils/ws"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	gpuCacheDuration = 30 * time.Second
)

// GPUStats represents statistics for a single GPU
type GPUStats struct {
	Name        string  `json:"name"`
	Index       int     `json:"index"`
	MemoryUsed  float64 `json:"memoryUsed"`
	MemoryTotal float64 `json:"memoryTotal"`
}

// SystemStats represents system resource statistics for WebSocket streaming.
type SystemStats struct {
	CPUUsage       float64         `json:"cpuUsage"`
	MemoryUsage    uint64          `json:"memoryUsage"`
	MemoryTotal    uint64          `json:"memoryTotal"`
	DiskUsage      uint64          `json:"diskUsage,omitempty"`
	DiskTotal      uint64          `json:"diskTotal,omitempty"`
	CPUCount       int             `json:"cpuCount"`
	Architecture   string          `json:"architecture"`
	Platform       string          `json:"platform"`
	Hostname       string          `json:"hostname,omitempty"`
	GPUCount       int             `json:"gpuCount"`
	GPUs           []GPUStats      `json:"gpus,omitempty"`
	Goroutines     GoroutineStats  `json:"goroutines"`
	Threads        uint64          `json:"threads"`
	Runtime        RuntimeStats    `json:"runtime"`
	RuntimeMetrics []RuntimeMetric `json:"runtimeMetrics,omitempty"`
}

// GoroutineStats represents Go scheduler statistics.
type GoroutineStats struct {
	Idle     uint64 `json:"idle"`
	Runnable uint64 `json:"runnable"`
	Running  uint64 `json:"running"`
	Syscall  uint64 `json:"syscall"`
	Waiting  uint64 `json:"waiting"`
	Total    uint64 `json:"total"`
	Created  uint64 `json:"created"`
}

// RuntimeStats represents detailed Go runtime metrics.
type RuntimeStats struct {
	HeapAlloc     uint64  `json:"heapAlloc"`
	HeapSys       uint64  `json:"heapSys"`
	HeapIdle      uint64  `json:"heapIdle"`
	HeapInuse     uint64  `json:"heapInuse"`
	StackInuse    uint64  `json:"stackInuse"`
	StackSys      uint64  `json:"stackSys"`
	MSpanInuse    uint64  `json:"mSpanInuse"`
	MSpanSys      uint64  `json:"mSpanSys"`
	MCacheInuse   uint64  `json:"mCacheInuse"`
	MCacheSys     uint64  `json:"mCacheSys"`
	GCSys         uint64  `json:"gcSys"`
	NextGC        uint64  `json:"nextGC"`
	LastGC        uint64  `json:"lastGC"`
	NumGC         uint32  `json:"numGC"`
	NumForcedGC   uint32  `json:"numForcedGC"`
	GCCPUFraction float64 `json:"gcCPUFraction"`
}

// RuntimeMetric represents a single runtime/metrics sample with metadata.
type RuntimeMetric struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Unit        string `json:"unit"`
	Kind        string `json:"kind"`
	Value       string `json:"value"`
}

// amdGPUSysfsPath is the base path for AMD GPU sysfs entries
const amdGPUSysfsPath = "/sys/class/drm"

// WebSocketHandler consolidates all WebSocket and streaming endpoints.
// REST endpoints are handled by Huma handlers.
type WebSocketHandler struct {
	projectService    *services.ProjectService
	containerService  *services.ContainerService
	systemService     *services.SystemService
	wsUpgrader        websocket.Upgrader
	activeConnections sync.Map
	cpuCache          struct {
		sync.RWMutex
		value     float64
		timestamp time.Time
	}
	diskUsagePathCache struct {
		sync.RWMutex
		value     string
		timestamp time.Time
	}
	gpuDetectionCache struct {
		sync.RWMutex
		detected  bool
		timestamp time.Time
		gpuType   string
		toolPath  string
	}
	detectionDone        bool
	detectionMutex       sync.Mutex
	gpuMonitoringEnabled bool
	gpuType              string
}

type wsLogStream struct {
	hub    *ws.Hub
	cancel context.CancelFunc
	format string
	seq    atomic.Uint64
}

func NewWebSocketHandler(
	group *gin.RouterGroup,
	projectService *services.ProjectService,
	containerService *services.ContainerService,
	systemService *services.SystemService,
	authMiddleware *middleware.AuthMiddleware,
	cfg *config.Config,
) {
	handler := &WebSocketHandler{
		projectService:       projectService,
		containerService:     containerService,
		systemService:        systemService,
		gpuMonitoringEnabled: cfg.GPUMonitoringEnabled,
		gpuType:              cfg.GPUType,
		wsUpgrader: websocket.Upgrader{
			CheckOrigin:       httputil.ValidateWebSocketOrigin(cfg.GetAppURL()),
			ReadBufferSize:    32 * 1024,
			WriteBufferSize:   32 * 1024,
			EnableCompression: true,
		},
	}

	wsGroup := group.Group("/environments/:id/ws")
	wsGroup.Use(authMiddleware.WithAdminNotRequired().Add())
	{
		wsGroup.GET("/projects/:projectId/logs", handler.ProjectLogs)
		wsGroup.GET("/containers/:containerId/logs", handler.ContainerLogs)
		wsGroup.GET("/containers/:containerId/stats", handler.ContainerStats)
		wsGroup.GET("/containers/:containerId/terminal", handler.ContainerExec)
		wsGroup.GET("/system/stats", handler.SystemStats)
	}
}

// ============================================================================
// Project WebSocket/Streaming Endpoints
// ============================================================================

// ProjectLogs streams project logs over WebSocket.
//
//	@Summary		Get project logs via WebSocket
//	@Description	Stream project logs over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			projectId	path	string	true	"Project ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/ws/projects/{projectId}/logs [get]
func (h *WebSocketHandler) ProjectLogs(c *gin.Context) {
	projectID := c.Param("projectId")
	if strings.TrimSpace(projectID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ProjectIDRequiredError{}).Error()})
		return
	}

	follow := c.DefaultQuery("follow", "true") == "true"
	tail, _ := httputil.GetQueryParam(c, "tail", false)
	if tail == "" {
		tail = "100"
	}
	since, _ := httputil.GetQueryParam(c, "since", false)
	timestamps := c.DefaultQuery("timestamps", "false") == "true"
	format, _ := httputil.GetQueryParam(c, "format", false)
	if format == "" {
		format = "text"
	}
	batched := c.DefaultQuery("batched", "false") == "true"

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub := h.startProjectLogHub(projectID, format, batched, follow, tail, since, timestamps)
	// WebSocket connections use context.Background() because they are long-lived and should not
	// be tied to the HTTP request context. Cleanup is handled via the hub's OnEmpty callback
	// which triggers when all clients disconnect.
	ws.ServeClient(context.Background(), hub, conn)
}

func (h *WebSocketHandler) startProjectLogHub(projectID, format string, batched, follow bool, tail, since string, timestamps bool) *ws.Hub {
	ls := &wsLogStream{
		hub:    ws.NewHub(1024),
		format: format,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel

	ls.hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up project log hub", "projectID", projectID)
		cancel()
	})

	go ls.hub.Run(ctx)

	lines := make(chan string, 256)
	go func(ctx context.Context) {
		defer close(lines)
		_ = h.projectService.StreamProjectLogs(ctx, projectID, lines, follow, tail, since, timestamps)
	}(ctx)

	if format == "json" {
		msgs := make(chan ws.LogMessage, 256)
		go func() {
			defer close(msgs)
			for line := range lines {
				level, service, msg, ts := ws.NormalizeProjectLine(line)
				seq := ls.seq.Add(1)
				timestamp := ts
				if timestamp == "" {
					timestamp = ws.NowRFC3339()
				}
				msgs <- ws.LogMessage{
					Seq:       seq,
					Level:     level,
					Message:   msg,
					Service:   service,
					Timestamp: timestamp,
				}
			}
		}()
		if batched {
			go ws.ForwardLogJSONBatched(ctx, ls.hub, msgs, 50, 400*time.Millisecond)
		} else {
			go ws.ForwardLogJSON(ctx, ls.hub, msgs)
		}
	} else {
		cleanChan := make(chan string, 256)
		go func() {
			defer close(cleanChan)
			for line := range lines {
				_, _, msg, _ := ws.NormalizeProjectLine(line)
				cleanChan <- msg
			}
		}()
		go ws.ForwardLines(ctx, ls.hub, cleanChan)
	}

	return ls.hub
}

// ============================================================================
// Container WebSocket Endpoints
// ============================================================================

// ContainerLogs streams container logs over WebSocket.
//
//	@Summary		Get container logs via WebSocket
//	@Description	Stream container logs over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/ws/containers/{containerId}/logs [get]
func (h *WebSocketHandler) ContainerLogs(c *gin.Context) {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ContainerIDRequiredError{}).Error()})
		return
	}

	follow := c.DefaultQuery("follow", "true") == "true"
	tail, _ := httputil.GetQueryParam(c, "tail", false)
	if tail == "" {
		tail = "100"
	}
	since, _ := httputil.GetQueryParam(c, "since", false)
	timestamps := c.DefaultQuery("timestamps", "false") == "true"
	format, _ := httputil.GetQueryParam(c, "format", false)
	if format == "" {
		format = "text"
	}
	batched := c.DefaultQuery("batched", "false") == "true"

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub := h.startContainerLogHub(containerID, format, batched, follow, tail, since, timestamps)
	// WebSocket connections use context.Background() because they are long-lived and should not
	// be tied to the HTTP request context. Cleanup is handled via the hub's OnEmpty callback
	// which triggers when all clients disconnect.
	ws.ServeClient(context.Background(), hub, conn)
}

func (h *WebSocketHandler) startContainerLogHub(containerID, format string, batched, follow bool, tail, since string, timestamps bool) *ws.Hub {
	ls := &wsLogStream{
		hub:    ws.NewHub(1024),
		format: format,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel

	ls.hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up container log hub", "containerID", containerID)
		cancel()
	})

	go ls.hub.Run(ctx)

	lines := make(chan string, 256)
	go func(ctx context.Context) {
		defer close(lines)
		_ = h.containerService.StreamLogs(ctx, containerID, lines, follow, tail, since, timestamps)
	}(ctx)

	if format == "json" {
		msgs := make(chan ws.LogMessage, 256)
		go func() {
			defer close(msgs)
			for line := range lines {
				level, msg, ts := ws.NormalizeContainerLine(line)
				seq := ls.seq.Add(1)
				timestamp := ts
				if timestamp == "" {
					timestamp = ws.NowRFC3339()
				}
				msgs <- ws.LogMessage{
					Seq:       seq,
					Level:     level,
					Message:   msg,
					Timestamp: timestamp,
				}
			}
		}()
		if batched {
			go ws.ForwardLogJSONBatched(ctx, ls.hub, msgs, 50, 400*time.Millisecond)
		} else {
			go ws.ForwardLogJSON(ctx, ls.hub, msgs)
		}
	} else {
		go ws.ForwardLines(ctx, ls.hub, lines)
	}

	return ls.hub
}

// ContainerStats streams container stats over WebSocket.
//
//	@Summary		Get container stats via WebSocket
//	@Description	Stream container resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Router			/api/environments/{id}/ws/containers/{containerId}/stats [get]
func (h *WebSocketHandler) ContainerStats(c *gin.Context) {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ContainerIDRequiredError{}).Error()})
		return
	}

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	hub := h.startContainerStatsHub(containerID)
	// WebSocket connections use context.Background() because they are long-lived and should not
	// be tied to the HTTP request context. Cleanup is handled via the hub's OnEmpty callback
	// which triggers when all clients disconnect.
	ws.ServeClient(context.Background(), hub, conn)
}

func (h *WebSocketHandler) startContainerStatsHub(containerID string) *ws.Hub {
	hub := ws.NewHub(64)

	ctx, cancel := context.WithCancel(context.Background())

	hub.SetOnEmpty(func() {
		slog.Debug("client disconnected, cleaning up container stats hub", "containerID", containerID)
		cancel()
	})

	go hub.Run(ctx)

	statsChan := make(chan interface{}, 64)
	go func(ctx context.Context) {
		defer close(statsChan)
		_ = h.containerService.StreamStats(ctx, containerID, statsChan)
	}(ctx)

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

	return hub
}

// ContainerExec provides interactive terminal access to a container.
//
//	@Summary		Execute command in container via WebSocket
//	@Description	Interactive terminal access to a container over WebSocket
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Param			shell		query	string	false	"Shell to execute"	default(/bin/sh)
//	@Router			/api/environments/{id}/ws/containers/{containerId}/terminal [get]
func (h *WebSocketHandler) ContainerExec(c *gin.Context) {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": (&common.ContainerIDRequiredError{}).Error()})
		return
	}

	shell := c.DefaultQuery("shell", "/bin/sh")

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	h.runContainerExecInternal(ctx, cancel, conn, containerID, shell)
}

func (h *WebSocketHandler) runContainerExecInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, containerID, shell string) {
	// Create exec instance
	execID, err := h.containerService.CreateExec(ctx, containerID, []string{shell})
	if err != nil {
		h.writeExecErrorInternal(conn, &common.ExecCreationError{Err: err})
		return
	}

	// Attach to exec
	execSession, err := h.containerService.AttachExec(ctx, containerID, execID)
	if err != nil {
		h.writeExecErrorInternal(conn, &common.ExecAttachError{Err: err})
		return
	}
	cleanup := h.execCleanupFuncInternal(ctx, execSession, execID, containerID)
	defer cleanup()
	h.watchExecContextInternal(ctx, execID, containerID, cleanup)

	done := make(chan struct{})
	go h.pipeExecOutputInternal(ctx, conn, execSession.Stdout(), execID, containerID, done)
	go h.pipeExecInputInternal(ctx, cancel, conn, execSession.Stdin(), execID, containerID)

	<-done
}

func (h *WebSocketHandler) writeExecErrorInternal(conn *websocket.Conn, err error) {
	_ = conn.WriteMessage(websocket.TextMessage, []byte(err.Error()+"\r\n"))
}

func (h *WebSocketHandler) execCleanupFuncInternal(ctx context.Context, execSession *services.ExecSession, execID, containerID string) func() {
	return func() {
		slog.Debug("Cleaning up exec session", "execID", execID, "containerID", containerID, "contextErr", ctx.Err())
		// Cleanup must proceed even if parent ctx is canceled.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:contextcheck
		defer cleanupCancel()
		if err := execSession.Close(cleanupCtx); err != nil { //nolint:contextcheck
			slog.Warn("Failed to clean up exec session", "execID", execID, "error", err)
		}
	}
}

func (h *WebSocketHandler) watchExecContextInternal(ctx context.Context, execID, containerID string, cleanup func()) {
	go func() {
		<-ctx.Done()
		slog.Debug("Exec context cancelled", "execID", execID, "containerID", containerID)
		cleanup()
	}()
}

func (h *WebSocketHandler) pipeExecOutputInternal(ctx context.Context, conn *websocket.Conn, stdout io.Reader, execID, containerID string, done chan<- struct{}) {
	defer close(done)
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := stdout.Read(buf)
		if err != nil {
			slog.Debug("Exec stdout read error", "execID", execID, "containerID", containerID, "error", err)
			return
		}
		if n > 0 {
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				slog.Debug("Exec websocket write error", "execID", execID, "containerID", containerID, "error", err)
				return
			}
		}
	}
}

func (h *WebSocketHandler) pipeExecInputInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, stdin io.Writer, execID, containerID string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			slog.Debug("Exec websocket read error", "execID", execID, "containerID", containerID, "error", err)
			cancel()
			return
		}
		if _, err := stdin.Write(data); err != nil {
			slog.Debug("Exec stdin write error", "execID", execID, "containerID", containerID, "error", err)
			return
		}
	}
}

// ============================================================================
// System WebSocket Endpoints
// ============================================================================

// checkRateLimit checks and applies rate limiting for WebSocket connections.
// Returns the counter and whether the connection should be allowed.
func (h *WebSocketHandler) checkRateLimit(clientIP string) (*int32, bool) {
	connCount, _ := h.activeConnections.LoadOrStore(clientIP, new(int32))
	count := connCount.(*int32)

	currentCount := atomic.AddInt32(count, 1)
	if currentCount > 5 {
		atomic.AddInt32(count, -1)
		return nil, false
	}
	return count, true
}

// releaseRateLimit decrements the connection counter and cleans up if needed.
func (h *WebSocketHandler) releaseRateLimit(clientIP string, count *int32) {
	newCount := atomic.AddInt32(count, -1)
	if newCount <= 0 {
		h.activeConnections.Delete(clientIP)
	}
}

// startCPUSampler starts a background goroutine that samples CPU usage.
func (h *WebSocketHandler) startCPUSampler(ctx context.Context, ticker *time.Ticker) {
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if vals, err := cpu.Percent(0, false); err == nil && len(vals) > 0 {
					h.cpuCache.Lock()
					h.cpuCache.value = vals[0]
					h.cpuCache.timestamp = time.Now()
					h.cpuCache.Unlock()
				}
			}
		}
	}(ctx)
}

// collectSystemStats gathers all system statistics.
func (h *WebSocketHandler) collectSystemStats(ctx context.Context, includeRuntimeMetrics bool) SystemStats {
	h.cpuCache.RLock()
	cpuUsage := h.cpuCache.value
	h.cpuCache.RUnlock()

	cpuCount := h.getCPUCount()
	memUsed, memTotal := h.getMemoryInfo()
	cpuCount, memUsed, memTotal = h.applyCgroupLimits(cpuCount, memUsed, memTotal)
	diskUsed, diskTotal := h.getDiskInfo(ctx)
	hostname := h.getHostname()
	gpuStats, gpuCount := h.getGPUInfo(ctx)
	goroutineStats, threadCount := h.getSchedulerStats()
	runtimeStats := h.getRuntimeStats()
	var runtimeMetrics []RuntimeMetric
	if includeRuntimeMetrics {
		runtimeMetrics = h.getRuntimeMetrics()
	}

	return SystemStats{
		CPUUsage:       cpuUsage,
		MemoryUsage:    memUsed,
		MemoryTotal:    memTotal,
		DiskUsage:      diskUsed,
		DiskTotal:      diskTotal,
		CPUCount:       cpuCount,
		Architecture:   runtime.GOARCH,
		Platform:       runtime.GOOS,
		Hostname:       hostname,
		GPUCount:       gpuCount,
		GPUs:           gpuStats,
		Goroutines:     goroutineStats,
		Threads:        threadCount,
		Runtime:        runtimeStats,
		RuntimeMetrics: runtimeMetrics,
	}
}

// getSchedulerStats collects Go scheduler metrics using runtime/metrics.
func (h *WebSocketHandler) getSchedulerStats() (GoroutineStats, uint64) {
	// Define the metrics we want to collect
	const (
		mIdle     = "/sched/goroutines/idle:goroutines"
		mRunnable = "/sched/goroutines/runnable:goroutines"
		mRunning  = "/sched/goroutines/running:goroutines"
		mSyscall  = "/sched/goroutines/syscall:goroutines"
		mWaiting  = "/sched/goroutines/waiting:goroutines"
		mTotal    = "/sched/goroutines:goroutines"
		mCreated  = "/sched/goroutines-created:goroutines"
		mThreads  = "/sched/threads:threads"
	)

	sample := make([]metrics.Sample, 8)
	sample[0].Name = mIdle
	sample[1].Name = mRunnable
	sample[2].Name = mRunning
	sample[3].Name = mSyscall
	sample[4].Name = mWaiting
	sample[5].Name = mTotal
	sample[6].Name = mCreated
	sample[7].Name = mThreads

	// Read the metrics
	metrics.Read(sample)

	var stats GoroutineStats
	var threads uint64

	// Extract values safely
	for _, s := range sample {
		val, ok := readUintSample(s)
		if !ok {
			continue
		}
		switch s.Name {
		case mIdle:
			stats.Idle = val
		case mRunnable:
			stats.Runnable = val
		case mRunning:
			stats.Running = val
		case mSyscall:
			stats.Syscall = val
		case mWaiting:
			stats.Waiting = val
		case mTotal:
			stats.Total = val
		case mCreated:
			stats.Created = val
		case mThreads:
			threads = val
		}
	}

	if stats.Total == 0 {
		stats.Total = uint64(runtime.NumGoroutine()) //nolint:gosec
	}
	if threads == 0 {
		threads = uint64(runtime.GOMAXPROCS(0)) //nolint:gosec
	}

	return stats, threads
}

func readUintSample(sample metrics.Sample) (uint64, bool) {
	const maxUint64 = ^uint64(0)
	const maxSafeJS = 9007199254740991

	switch sample.Value.Kind() {
	case metrics.KindBad:
		return 0, false
	case metrics.KindUint64:
		val := sample.Value.Uint64()
		if val == maxUint64 || val > maxSafeJS {
			return 0, false
		}
		return val, true
	case metrics.KindFloat64:
		val := sample.Value.Float64()
		if math.IsNaN(val) || math.IsInf(val, 0) || val < 0 {
			return 0, false
		}
		if val > float64(maxSafeJS) {
			return 0, false
		}
		return uint64(val), true
	case metrics.KindFloat64Histogram:
		return 0, false
	default:
		return 0, false
	}
}

// getRuntimeStats collects Go runtime memory and GC metrics.
func (h *WebSocketHandler) getRuntimeStats() RuntimeStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return RuntimeStats{
		HeapAlloc:     m.HeapAlloc,
		HeapSys:       m.HeapSys,
		HeapIdle:      m.HeapIdle,
		HeapInuse:     m.HeapInuse,
		StackInuse:    m.StackInuse,
		StackSys:      m.StackSys,
		MSpanInuse:    m.MSpanInuse,
		MSpanSys:      m.MSpanSys,
		MCacheInuse:   m.MCacheInuse,
		MCacheSys:     m.MCacheSys,
		GCSys:         m.GCSys,
		NextGC:        m.NextGC,
		LastGC:        m.LastGC,
		NumGC:         m.NumGC,
		NumForcedGC:   m.NumForcedGC,
		GCCPUFraction: m.GCCPUFraction,
	}
}

func (h *WebSocketHandler) getRuntimeMetrics() []RuntimeMetric {
	descriptions := metrics.All()
	samples := make([]metrics.Sample, len(descriptions))
	for i, desc := range descriptions {
		samples[i].Name = desc.Name
	}

	metrics.Read(samples)
	metricsList := make([]RuntimeMetric, 0, len(samples))

	for i, sample := range samples {
		desc := descriptions[i]
		metricName := sample.Name
		metricUnit := ""
		if parts := strings.SplitN(sample.Name, ":", 2); len(parts) == 2 {
			metricName = parts[0]
			metricUnit = parts[1]
		}
		metric := RuntimeMetric{
			Name:        metricName,
			Description: desc.Description,
			Unit:        metricUnit,
			Kind:        metricKindString(sample.Value.Kind()),
			Value:       formatMetricValue(sample),
		}
		metricsList = append(metricsList, metric)
	}

	return metricsList
}

func metricKindString(kind metrics.ValueKind) string {
	switch kind {
	case metrics.KindBad:
		return "bad"
	case metrics.KindUint64:
		return "uint64"
	case metrics.KindFloat64:
		return "float64"
	case metrics.KindFloat64Histogram:
		return "float64-histogram"
	default:
		return "bad"
	}
}

func formatMetricValue(sample metrics.Sample) string {
	switch sample.Value.Kind() {
	case metrics.KindBad:
		return ""
	case metrics.KindUint64:
		return strconv.FormatUint(sample.Value.Uint64(), 10)
	case metrics.KindFloat64:
		val := sample.Value.Float64()
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return ""
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case metrics.KindFloat64Histogram:
		hist := sample.Value.Float64Histogram()
		if hist == nil {
			return ""
		}
		var total uint64
		for _, count := range hist.Counts {
			total += count
		}
		return fmt.Sprintf("buckets=%d samples=%d", len(hist.Counts), total)
	default:
		return ""
	}
}

// getCPUCount returns the number of CPUs.
func (h *WebSocketHandler) getCPUCount() int {
	cpuCount, err := cpu.Counts(true)
	if err != nil {
		return runtime.NumCPU()
	}
	return cpuCount
}

// getMemoryInfo returns memory usage and total.
func (h *WebSocketHandler) getMemoryInfo() (uint64, uint64) {
	memInfo, _ := mem.VirtualMemory()
	if memInfo == nil {
		return 0, 0
	}
	return memInfo.Used, memInfo.Total
}

// applyCgroupLimits applies cgroup limits when running in a container.
func (h *WebSocketHandler) applyCgroupLimits(cpuCount int, memUsed, memTotal uint64) (int, uint64, uint64) {
	cgroupLimits, err := docker.DetectCgroupLimits()
	if err != nil {
		return cpuCount, memUsed, memTotal
	}

	if limit := cgroupLimits.MemoryLimit; limit > 0 {
		limitUint := uint64(limit)
		if memTotal == 0 || limitUint < memTotal {
			memTotal = limitUint
			if cgroupLimits.MemoryUsage > 0 {
				memUsed = uint64(cgroupLimits.MemoryUsage)
			}
		}
	}
	if cgroupLimits.CPUCount > 0 && (cpuCount == 0 || cgroupLimits.CPUCount < cpuCount) {
		cpuCount = cgroupLimits.CPUCount
	}
	return cpuCount, memUsed, memTotal
}

// getDiskInfo returns disk usage and total.
func (h *WebSocketHandler) getDiskInfo(ctx context.Context) (uint64, uint64) {
	diskUsagePath := h.getDiskUsagePath(ctx)
	diskInfo, err := disk.Usage(diskUsagePath)
	if err != nil || diskInfo == nil || diskInfo.Total == 0 {
		if diskUsagePath != "/" {
			diskInfo, _ = disk.Usage("/")
		}
	}
	if diskInfo == nil {
		return 0, 0
	}
	return diskInfo.Used, diskInfo.Total
}

// getHostname returns the system hostname.
func (h *WebSocketHandler) getHostname() string {
	hostInfo, _ := host.Info()
	if hostInfo == nil {
		return ""
	}
	return hostInfo.Hostname
}

// getGPUInfo returns GPU statistics if monitoring is enabled.
func (h *WebSocketHandler) getGPUInfo(ctx context.Context) ([]GPUStats, int) {
	if !h.gpuMonitoringEnabled {
		return nil, 0
	}
	gpuData, err := h.getGPUStats(ctx)
	if err != nil {
		return nil, 0
	}
	return gpuData, len(gpuData)
}

// initializeCPUCache performs initial CPU sampling.
func (h *WebSocketHandler) initializeCPUCache() {
	if vals, err := cpu.Percent(time.Second, false); err == nil && len(vals) > 0 {
		h.cpuCache.Lock()
		h.cpuCache.value = vals[0]
		h.cpuCache.timestamp = time.Now()
		h.cpuCache.Unlock()
	}
}

// SystemStats streams system stats over WebSocket.
//
//	@Summary		Get system stats via WebSocket
//	@Description	Stream system resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id	path	string	true	"Environment ID"
//	@Router			/api/environments/{id}/ws/system/stats [get]
func (h *WebSocketHandler) SystemStats(c *gin.Context) {
	clientIP := c.ClientIP()

	count, allowed := h.checkRateLimit(clientIP)
	if !allowed {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"error":   "Too many concurrent stats connections from this IP",
		})
		return
	}
	defer h.releaseRateLimit(clientIP, count)

	conn, err := h.wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	interval, _ := httputil.GetIntQueryParam(c, "interval", false)
	if interval <= 0 {
		interval = 2
	}

	includeRuntimeMetrics := false
	if runtimeMetricsParam, _ := httputil.GetQueryParam(c, "runtimeMetrics", false); runtimeMetricsParam != "" {
		param := strings.ToLower(runtimeMetricsParam)
		includeRuntimeMetrics = param == "1" || param == "true" || param == "yes"
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	cpuUpdateTicker := time.NewTicker(1 * time.Second)
	defer cpuUpdateTicker.Stop()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	h.startCPUSampler(ctx, cpuUpdateTicker)

	send := func() error {
		stats := h.collectSystemStats(ctx, includeRuntimeMetrics)
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(stats)
	}

	h.initializeCPUCache()

	if err := send(); err != nil {
		return
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if err := send(); err != nil {
				return
			}
		}
	}
}

func (h *WebSocketHandler) getDiskUsagePath(ctx context.Context) string {
	h.diskUsagePathCache.RLock()
	if h.diskUsagePathCache.value != "" && time.Since(h.diskUsagePathCache.timestamp) < 5*time.Minute {
		path := h.diskUsagePathCache.value
		h.diskUsagePathCache.RUnlock()
		return path
	}
	h.diskUsagePathCache.RUnlock()

	// Default path
	path := "/"

	// Try to get Docker root from system service
	if h.systemService != nil {
		path = h.systemService.GetDiskUsagePath(ctx)
	}

	h.diskUsagePathCache.Lock()
	h.diskUsagePathCache.value = path
	h.diskUsagePathCache.timestamp = time.Now()
	h.diskUsagePathCache.Unlock()

	return path
}

// ============================================================================
// GPU Monitoring
// ============================================================================

// getGPUStats collects and returns GPU statistics for all available GPUs
func (h *WebSocketHandler) getGPUStats(ctx context.Context) ([]GPUStats, error) {
	h.detectionMutex.Lock()
	done := h.detectionDone
	h.detectionMutex.Unlock()

	if !done {
		if err := h.detectGPUs(ctx); err != nil {
			return nil, err
		}
	}

	h.gpuDetectionCache.RLock()
	if h.gpuDetectionCache.detected && time.Since(h.gpuDetectionCache.timestamp) < gpuCacheDuration {
		gpuType := h.gpuDetectionCache.gpuType
		h.gpuDetectionCache.RUnlock()

		switch gpuType {
		case "nvidia":
			return h.getNvidiaStats(ctx)
		case "amd":
			return h.getAMDStats(ctx)
		case "intel":
			return h.getIntelStats(ctx)
		}
	}
	h.gpuDetectionCache.RUnlock()

	if err := h.detectGPUs(ctx); err != nil {
		return nil, err
	}

	h.gpuDetectionCache.RLock()
	gpuType := h.gpuDetectionCache.gpuType
	h.gpuDetectionCache.RUnlock()

	switch gpuType {
	case "nvidia":
		return h.getNvidiaStats(ctx)
	case "amd":
		return h.getAMDStats(ctx)
	case "intel":
		return h.getIntelStats(ctx)
	default:
		return nil, fmt.Errorf("no supported GPU found")
	}
}

// detectGPUs detects available GPU management tools
func (h *WebSocketHandler) detectGPUs(ctx context.Context) error {
	h.detectionMutex.Lock()
	defer h.detectionMutex.Unlock()

	if h.gpuType != "" && h.gpuType != "auto" {
		switch h.gpuType {
		case "nvidia":
			if path, err := exec.LookPath("nvidia-smi"); err == nil {
				h.gpuDetectionCache.Lock()
				h.gpuDetectionCache.detected = true
				h.gpuDetectionCache.gpuType = "nvidia"
				h.gpuDetectionCache.toolPath = path
				h.gpuDetectionCache.timestamp = time.Now()
				h.gpuDetectionCache.Unlock()
				h.detectionDone = true
				slog.InfoContext(ctx, "Using configured GPU type", "type", "nvidia")
				return nil
			}
			return fmt.Errorf("nvidia-smi not found but GPU_TYPE set to nvidia")

		case "amd":
			if hasAMDGPUInternal() {
				h.gpuDetectionCache.Lock()
				h.gpuDetectionCache.detected = true
				h.gpuDetectionCache.gpuType = "amd"
				h.gpuDetectionCache.toolPath = amdGPUSysfsPath
				h.gpuDetectionCache.timestamp = time.Now()
				h.gpuDetectionCache.Unlock()
				h.detectionDone = true
				slog.InfoContext(ctx, "Using configured GPU type", "type", "amd")
				return nil
			}
			return fmt.Errorf("AMD GPU not found in sysfs but GPU_TYPE set to amd")

		case "intel":
			if path, err := exec.LookPath("intel_gpu_top"); err == nil {
				h.gpuDetectionCache.Lock()
				h.gpuDetectionCache.detected = true
				h.gpuDetectionCache.gpuType = "intel"
				h.gpuDetectionCache.toolPath = path
				h.gpuDetectionCache.timestamp = time.Now()
				h.gpuDetectionCache.Unlock()
				h.detectionDone = true
				slog.InfoContext(ctx, "Using configured GPU type", "type", "intel")
				return nil
			}
			return fmt.Errorf("intel_gpu_top not found but GPU_TYPE set to intel")

		default:
			slog.WarnContext(ctx, "Invalid GPU_TYPE specified, falling back to auto-detection", "gpu_type", h.gpuType)
		}
	}

	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "nvidia"
		h.gpuDetectionCache.toolPath = path
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "NVIDIA GPU detected", "tool", "nvidia-smi", "path", path)
		return nil
	}

	if hasAMDGPUInternal() {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "amd"
		h.gpuDetectionCache.toolPath = amdGPUSysfsPath
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "AMD GPU detected", "method", "sysfs", "path", amdGPUSysfsPath)
		return nil
	}

	if path, err := exec.LookPath("intel_gpu_top"); err == nil {
		h.gpuDetectionCache.Lock()
		h.gpuDetectionCache.detected = true
		h.gpuDetectionCache.gpuType = "intel"
		h.gpuDetectionCache.toolPath = path
		h.gpuDetectionCache.timestamp = time.Now()
		h.gpuDetectionCache.Unlock()
		h.detectionDone = true
		slog.InfoContext(ctx, "Intel GPU detected", "tool", "intel_gpu_top", "path", path)
		return nil
	}

	h.detectionDone = true
	return fmt.Errorf("no supported GPU found")
}

// getNvidiaStats collects NVIDIA GPU statistics using nvidia-smi
func (h *WebSocketHandler) getNvidiaStats(ctx context.Context) ([]GPUStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,memory.used,memory.total",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		slog.WarnContext(ctx, "Failed to execute nvidia-smi", "error", err)
		return nil, fmt.Errorf("nvidia-smi execution failed: %w", err)
	}

	reader := csv.NewReader(bytes.NewReader(output))
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse nvidia-smi CSV output", "error", err)
		return nil, fmt.Errorf("failed to parse nvidia-smi output: %w", err)
	}

	var stats []GPUStats
	for _, record := range records {
		if len(record) < 4 {
			continue
		}

		index, err := strconv.Atoi(strings.TrimSpace(record[0]))
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse GPU index", "value", record[0])
			continue
		}

		name := strings.TrimSpace(record[1])

		memUsed, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse memory used", "value", record[2])
			continue
		}

		memTotal, err := strconv.ParseFloat(strings.TrimSpace(record[3]), 64)
		if err != nil {
			slog.WarnContext(ctx, "Failed to parse memory total", "value", record[3])
			continue
		}

		stats = append(stats, GPUStats{
			Name:        name,
			Index:       index,
			MemoryUsed:  memUsed * 1024 * 1024,
			MemoryTotal: memTotal * 1024 * 1024,
		})
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no GPU data parsed from nvidia-smi")
	}

	slog.DebugContext(ctx, "Collected NVIDIA GPU stats", "gpu_count", len(stats))
	return stats, nil
}

// getAMDStats collects AMD GPU statistics using sysfs
func (h *WebSocketHandler) getAMDStats(ctx context.Context) ([]GPUStats, error) {
	var stats []GPUStats

	// Find AMD GPU cards by looking for mem_info_vram_total in /sys/class/drm/card*/device/
	entries, err := os.ReadDir(amdGPUSysfsPath)
	if err != nil {
		slog.WarnContext(ctx, "Failed to read DRM sysfs directory", "error", err)
		return nil, fmt.Errorf("failed to read sysfs: %w", err)
	}

	index := 0
	for _, entry := range entries {
		name := entry.Name()
		// Only check card* entries (card0, card1, etc.) - skip renderD* and connector entries
		if !strings.HasPrefix(name, "card") {
			continue
		}
		// Skip connector entries like card0-DP-1, card0-HDMI-A-1
		if strings.Contains(name, "-") {
			continue
		}

		devicePath := fmt.Sprintf("%s/%s/device", amdGPUSysfsPath, name)

		// Check if this is an AMD GPU by looking for VRAM info files
		memTotalPath := fmt.Sprintf("%s/mem_info_vram_total", devicePath)
		memUsedPath := fmt.Sprintf("%s/mem_info_vram_used", devicePath)

		memTotalBytes, err := readSysfsValueInternal(memTotalPath)
		if err != nil {
			// Not an AMD GPU or doesn't have VRAM info
			continue
		}

		memUsedBytes, err := readSysfsValueInternal(memUsedPath)
		if err != nil {
			slog.WarnContext(ctx, "Failed to read AMD GPU memory used", "card", name, "error", err)
			continue
		}

		stats = append(stats, GPUStats{
			Name:        fmt.Sprintf("AMD GPU %d", index),
			Index:       index,
			MemoryUsed:  float64(memUsedBytes),
			MemoryTotal: float64(memTotalBytes),
		})
		index++
	}

	if len(stats) == 0 {
		return nil, fmt.Errorf("no AMD GPU data found in sysfs")
	}

	slog.DebugContext(ctx, "Collected AMD GPU stats", "gpu_count", len(stats))
	return stats, nil
}

// readSysfsValueInternal reads a numeric value from a sysfs file
func readSysfsValueInternal(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// hasAMDGPUInternal checks if an AMD GPU is present by looking for VRAM info in sysfs
func hasAMDGPUInternal() bool {
	entries, err := os.ReadDir(amdGPUSysfsPath)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		name := entry.Name()
		// Only check card* entries (card0, card1, etc.) - skip card0-DP-1 style entries
		if !strings.HasPrefix(name, "card") {
			continue
		}
		// Skip connector entries like card0-DP-1, card0-HDMI-A-1
		if strings.Contains(name, "-") {
			continue
		}

		// Check if this card has AMD VRAM info
		memTotalPath := fmt.Sprintf("%s/%s/device/mem_info_vram_total", amdGPUSysfsPath, name)
		if _, err := os.Stat(memTotalPath); err == nil {
			return true
		}
	}

	return false
}

// getIntelStats collects Intel GPU statistics using intel_gpu_top
func (h *WebSocketHandler) getIntelStats(ctx context.Context) ([]GPUStats, error) {
	stats := []GPUStats{
		{
			Name:        "Intel GPU",
			Index:       0,
			MemoryUsed:  0,
			MemoryTotal: 0,
		},
	}

	slog.DebugContext(ctx, "Intel GPU detected but detailed stats not yet implemented")
	return stats, nil
}
