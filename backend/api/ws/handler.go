package ws

import (
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"emperror.dev/errors"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"
	"github.com/samber/hot"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/hostshell"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/system"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	httputil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/sys/cgroup"
)

const cgroupCacheTTL = 30 * time.Second

var defaultWebSocketMetrics = wshub.NewWebSocketMetrics()

// ============================================================================
// WebSocket Handler
// ============================================================================

// WebSocketHandler consolidates all WebSocket and streaming endpoints.
// REST endpoints are handled by Huma handlers.
type WebSocketHandler struct {
	projectService     *services.ProjectService
	containerService   *services.ContainerService
	swarmService       *services.SwarmService
	systemService      *services.SystemService
	diagnosticsService *services.DiagnosticsService
	hostShellService   *services.HostShellService
	checkWSOrigin      func(*http.Request) bool
	wsMetrics          *wshub.WebSocketMetrics
	activeConnections  sync.Map
	logStreamsMu       sync.Mutex
	logStreams         map[string]*wsLogStream
	cpuCache           struct {
		sync.RWMutex

		value     float64
		timestamp time.Time
	}
	systemStaticInfo struct {
		once     sync.Once
		cpuCount int
		hostname string
	}
	systemStatsSampler struct {
		stateMu     sync.RWMutex
		latest      systemtypes.SystemStats
		timestamp   time.Time
		lifecycleMu sync.Mutex
		clients     int
		cancel      context.CancelFunc
		ready       chan struct{}
		running     bool
	}
	containerStatsHubs sync.Map
	cgroupCache        *cgroup.Cache
	gpuMonitor         *system.GPUMonitor

	diskUsagePathCache   *hot.HotCache[struct{}, string]
	projectLogStreamer   func(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error
	containerLogStreamer func(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error
	systemStatsCollector func(ctx context.Context) systemtypes.SystemStats
	cpuUsageReader       func(interval time.Duration) (float64, bool)
}

type wsLogStream struct {
	hub             *wshub.Hub
	cancel          context.CancelFunc
	firstSubscriber chan struct{}
	format          string
	key             string
	refs            int
	done            bool
	seq             atomic.Uint64
}

func getContextUserIDInternal(c *echo.Context) string {
	if val := c.Get("userID"); val != nil {
		if userID, ok := val.(string); ok {
			return userID
		}
	}
	return ""
}

func getContextUsernameInternal(c *echo.Context) string {
	if val := c.Get("currentUser"); val != nil {
		if user, ok := val.(*models.User); ok {
			return user.Username
		}
	}
	return ""
}

func buildWSConnectionInfoInternal(c *echo.Context, kind, resourceID string) systemtypes.WebSocketConnectionInfo {
	return systemtypes.WebSocketConnectionInfo{
		Kind:       kind,
		EnvID:      c.Param("id"),
		ResourceID: resourceID,
		ClientIP:   c.RealIP(),
		UserID:     getContextUserIDInternal(c),
		UserAgent:  c.Request().Header.Get("User-Agent"),
	}
}

func buildLogStreamKeyInternal(envID, kind, resourceID, format string, batched, follow bool, tail, since string, timestamps bool) string {
	return strings.Join([]string{
		envID,
		kind,
		resourceID,
		format,
		strconv.FormatBool(batched),
		strconv.FormatBool(follow),
		tail,
		since,
		strconv.FormatBool(timestamps),
	}, "|")
}

func (h *WebSocketHandler) streamProjectLogsInternal(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	if h.projectLogStreamer != nil {
		return h.projectLogStreamer(ctx, projectID, logsChan, follow, tail, since, timestamps)
	}
	return h.projectService.StreamProjectLogs(ctx, projectID, logsChan, follow, tail, since, timestamps)
}

func (h *WebSocketHandler) streamContainerLogsInternal(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error {
	if h.containerLogStreamer != nil {
		return h.containerLogStreamer(ctx, containerID, logsChan, follow, tail, since, timestamps)
	}
	return h.containerService.StreamLogs(ctx, containerID, logsChan, follow, tail, since, timestamps)
}

func (h *WebSocketHandler) getOrCreateLogStreamInternal(key string, create func(onEmpty func(*wsLogStream)) *wsLogStream) *wsLogStream {
	h.logStreamsMu.Lock()
	defer h.logStreamsMu.Unlock()

	if stream, ok := h.logStreams[key]; ok {
		if !stream.done {
			stream.refs++
			return stream
		}
	}

	stream := create(func(stream *wsLogStream) {
		h.markLogStreamDoneInternal(key, stream)
	})
	stream.key = key
	stream.refs = 1
	h.logStreams[key] = stream
	return stream
}

func takeLogStreamCancelInternal(stream *wsLogStream) context.CancelFunc {
	cancel := stream.cancel
	stream.cancel = nil
	return cancel
}

func (h *WebSocketHandler) releaseLogStreamInternal(key string, stream *wsLogStream) {
	var cancel context.CancelFunc

	h.logStreamsMu.Lock()
	if stream.refs > 0 {
		stream.refs--
	}
	if stream.refs == 0 {
		if current, ok := h.logStreams[key]; ok && current == stream {
			delete(h.logStreams, key)
		}
		cancel = takeLogStreamCancelInternal(stream)
	}
	h.logStreamsMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (h *WebSocketHandler) markLogStreamDoneInternal(key string, stream *wsLogStream) {
	var cancel context.CancelFunc

	h.logStreamsMu.Lock()
	stream.done = true
	if stream.refs == 0 {
		if current, ok := h.logStreams[key]; ok && current == stream {
			delete(h.logStreams, key)
		}
		cancel = takeLogStreamCancelInternal(stream)
	}
	h.logStreamsMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func NewWebSocketHandler(
	group *echo.Group,
	projectService *services.ProjectService,
	containerService *services.ContainerService,
	swarmService *services.SwarmService,
	systemService *services.SystemService,
	diagnosticsService *services.DiagnosticsService,
	hostShellService *services.HostShellService,
	authMiddleware *middleware.AuthMiddleware,
	cfg *config.Config,
) {
	handler := &WebSocketHandler{
		projectService:     projectService,
		containerService:   containerService,
		swarmService:       swarmService,
		systemService:      systemService,
		diagnosticsService: diagnosticsService,
		hostShellService:   hostShellService,
		wsMetrics:          defaultWebSocketMetrics,
		logStreams:         make(map[string]*wsLogStream),
		cgroupCache:        cgroup.NewCache(cgroupCacheTTL),
		gpuMonitor:         system.NewGPUMonitor(cfg.GPUMonitoringEnabled, cfg.GPUType),
		diskUsagePathCache: hot.NewHotCache[struct{}, string](hot.LRU, 1).
			WithTTL(5 * time.Minute).
			Build(),
		checkWSOrigin: httputil.ValidateWebSocketOrigin(cfg.GetAppURL()),
	}
	wsGroup := group.Group("/environments/:id/ws", authMiddleware.WithAdminNotRequired().Add())
	for _, r := range handler.proxiedRoutes() {
		wsGroup.GET(r.path, r.handler, middleware.RequirePermission(r.perm))
	}
	handler.registerDiagnosticsRoutesInternal(group, authMiddleware)
}

// ============================================================================
// Shared Log Stream Helpers
// ============================================================================

// logStreamParams holds the standard query parameters shared by every WS log endpoint.
type logStreamParams struct {
	tail       string
	since      string
	format     string
	follow     bool
	timestamps bool
	batched    bool
}

func parseLogStreamParamsInternal(c *echo.Context) logStreamParams {
	req := c.Request()
	tail, _ := httputil.GetQueryParam(req, "tail", false)
	if tail == "" {
		tail = "100"
	}
	since, _ := httputil.GetQueryParam(req, "since", false)
	format, _ := httputil.GetQueryParam(req, "format", false)
	if format == "" {
		format = "text"
	}
	return logStreamParams{
		follow:     queryParamWithDefaultInternal(c, "follow", "true") == "true",
		tail:       tail,
		since:      since,
		timestamps: queryParamWithDefaultInternal(c, "timestamps", "false") == "true",
		format:     format,
		batched:    queryParamWithDefaultInternal(c, "batched", "false") == "true",
	}
}

func queryParamWithDefaultInternal(c *echo.Context, key, def string) string {
	if v := c.QueryParam(key); v != "" {
		return v
	}
	return def
}

// serveLogStreamInternal is the shared scaffold for all three WS log endpoints (project, container, service).
// It performs upgrade, builds the stream key, gets-or-creates the multiplexing hub, registers metrics,
// and serves the client. The caller-supplied hubBuilder constructs the underlying *wsLogStream
// when no hub already exists for streamKey.
func (h *WebSocketHandler) serveLogStreamInternal(
	c *echo.Context,
	kind, resourceID string,
	params logStreamParams,
	hubBuilder func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream,
) {
	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		return
	}

	streamKey := buildLogStreamKeyInternal(c.Param("id"), kind, resourceID, params.format, params.batched, params.follow, params.tail, params.since, params.timestamps)
	stream := h.getOrCreateLogStreamInternal(streamKey, func(onEmpty func(*wsLogStream)) *wsLogStream {
		return hubBuilder(streamKey, onEmpty)
	})
	connID := h.wsMetrics.RegisterConnection(buildWSConnectionInfoInternal(c, kind, resourceID))
	release := func() {
		h.wsMetrics.UnregisterConnection(connID)
		h.releaseLogStreamInternal(streamKey, stream)
	}
	// WebSocket connections use context.Background() because they are long-lived and should not
	// be tied to the HTTP request context. Cleanup is handled via the hub's OnEmpty callback
	// which triggers when all clients disconnect.
	if !wshub.ServeClientWithOnRemove(context.Background(), stream.hub, conn, release) {
		// The stream refcount normally keeps this hub alive, so a stopped hub
		// here means it was torn down out from under us; drop our reference
		// rather than leaking the connection and the metrics entry.
		slog.Debug("log stream hub stopped before client registration", "streamKey", streamKey)
		_ = conn.CloseNow()
		release()
	}
}

// broadcastLogStreamErrorInternal emits an error message to every client of a log stream.
// resourceLabel is the human-readable noun used in slog/error text (e.g. "project log stream").
// errorPrefix is the user-facing message prefix (e.g. "Failed to stream project logs: ").
func broadcastLogStreamErrorInternal(resourceLabel, errorPrefix string, resourceID string, format string, err error, ls *wsLogStream) {
	slog.Warn(resourceLabel+" failed", "resourceID", resourceID, "error", err)

	if format == "json" {
		msg := wshub.LogMessage{
			Seq:       ls.seq.Add(1),
			Level:     "error",
			Message:   errorPrefix + err.Error(),
			Service:   "arcane",
			Timestamp: wshub.NowRFC3339(),
		}
		if b, marshalErr := json.Marshal(msg); marshalErr == nil {
			ls.hub.Broadcast(b)
		}
		return
	}

	ls.hub.Broadcast([]byte(errorPrefix + err.Error()))
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
func (h *WebSocketHandler) ProjectLogs(c *echo.Context) error {
	projectID := c.Param("projectId")
	if strings.TrimSpace(projectID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Project ID is required"})
	}

	params := parseLogStreamParamsInternal(c)
	h.serveLogStreamInternal(c, systemtypes.WSKindProjectLogs, projectID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHubInternal(
			streamKey,
			projectID,
			"project",
			params,
			h.streamProjectLogsInternal,
			normalizeProjectLogMessageInternal,
			normalizeProjectLogTextInternal,
			onEmpty,
		)
	})
	return nil
}

func newWSLogStreamInternal(key, format string) (*wsLogStream, context.Context) {
	ls := &wsLogStream{
		hub:             wshub.NewHub(1024),
		firstSubscriber: make(chan struct{}),
		format:          format,
		key:             key,
	}
	ls.hub.SetOnFirstClient(func() {
		close(ls.firstSubscriber)
	})

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is intentionally retained and invoked by the hub OnEmpty callback.
	ls.cancel = cancel

	go ls.hub.Run(ctx)

	return ls, ctx
}

func waitForLogStreamSubscriberInternal(ctx context.Context, firstSubscriber <-chan struct{}) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-firstSubscriber:
			return true
		}
	}
}

func normalizeProjectLogMessageInternal(line string) wshub.LogMessage {
	level, service, message, timestamp := wshub.NormalizeProjectLine(line)
	return wshub.LogMessage{
		Level:     level,
		Message:   message,
		Service:   service,
		Timestamp: timestamp,
	}
}

func normalizeContainerLogMessageInternal(line string) wshub.LogMessage {
	level, message, timestamp := wshub.NormalizeContainerLine(line)
	return wshub.LogMessage{
		Level:     level,
		Message:   message,
		Timestamp: timestamp,
	}
}

func normalizeProjectLogTextInternal(line string) string {
	_, _, message, _ := wshub.NormalizeProjectLine(line)
	return message
}

func (h *WebSocketHandler) startLogHubInternal(
	key, resourceID, label string,
	params logStreamParams,
	stream func(context.Context, string, chan<- string, bool, string, string, bool) error,
	normalizeJSON func(string) wshub.LogMessage,
	normalizeText func(string) string,
	onEmptyHook func(*wsLogStream),
) *wsLogStream {
	ls, ctx := newWSLogStreamInternal(key, params.format)

	ls.hub.SetOnEmpty(func() {
		if onEmptyHook != nil {
			onEmptyHook(ls)
		}
		slog.Debug("client disconnected, cleaning up "+label+" log hub", label+"ID", resourceID)
	})

	lines := h.startLogSourceInternal(ctx, key, resourceID, label, params, stream, ls)
	startLogForwardersInternal(ctx, ls, lines, params, normalizeJSON, normalizeText)

	return ls
}

func (h *WebSocketHandler) startLogSourceInternal(
	ctx context.Context,
	key, resourceID, label string,
	params logStreamParams,
	stream func(context.Context, string, chan<- string, bool, string, string, bool) error,
	ls *wsLogStream,
) <-chan string {
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		if !waitForLogStreamSubscriberInternal(ctx, ls.firstSubscriber) {
			return
		}

		if err := stream(ctx, resourceID, lines, params.follow, params.tail, params.since, params.timestamps); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}

			h.markLogStreamDoneInternal(key, ls)
			broadcastLogStreamErrorInternal(label+" log stream", "Failed to stream "+label+" logs: ", resourceID, params.format, err, ls)
			return
		}

		if ctx.Err() == nil {
			h.markLogStreamDoneInternal(key, ls)
		}
	}()

	return lines
}

func startLogForwardersInternal(
	ctx context.Context,
	ls *wsLogStream,
	lines <-chan string,
	params logStreamParams,
	normalizeJSON func(string) wshub.LogMessage,
	normalizeText func(string) string,
) {
	if params.format == "json" {
		messages := mapLogLinesInternal(ctx, lines, func(line string) wshub.LogMessage {
			message := normalizeJSON(line)
			message.Seq = ls.seq.Add(1)
			if message.Timestamp == "" {
				message.Timestamp = wshub.NowRFC3339()
			}
			return message
		})

		if params.batched {
			go wshub.ForwardLogJSONBatched(ctx, ls.hub, messages, 50, 400*time.Millisecond)
		} else {
			go wshub.ForwardLogJSON(ctx, ls.hub, messages)
		}

		return
	}

	textLines := lines
	if normalizeText != nil {
		textLines = mapLogLinesInternal(ctx, lines, normalizeText)
	}
	go wshub.ForwardLines(ctx, ls.hub, textLines)
}

func mapLogLinesInternal[T any](ctx context.Context, lines <-chan string, transform func(string) T) <-chan T {
	mapped := make(chan T, 256)
	go func() {
		defer close(mapped)
		for {
			select {
			case <-ctx.Done():
				return
			case line, ok := <-lines:
				if !ok {
					return
				}

				select {
				case <-ctx.Done():
					return
				case mapped <- transform(line):
				}
			}
		}
	}()

	return mapped
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
func (h *WebSocketHandler) ContainerLogs(c *echo.Context) error {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Container ID is required"})
	}

	params := parseLogStreamParamsInternal(c)
	h.serveLogStreamInternal(c, systemtypes.WSKindContainerLogs, containerID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHubInternal(
			streamKey,
			containerID,
			"container",
			params,
			h.streamContainerLogsInternal,
			normalizeContainerLogMessageInternal,
			nil,
			onEmpty,
		)
	})
	return nil
}

// ============================================================================
// Swarm Service WebSocket/Streaming Endpoints
// ============================================================================

// ServiceLogs streams swarm service logs over WebSocket.
//
//	@Summary		Get swarm service logs via WebSocket
//	@Description	Stream swarm service logs over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			serviceId	path	string	true	"Service ID"
//	@Param			follow		query	bool	false	"Follow log output"						default(true)
//	@Param			tail		query	string	false	"Number of lines to show from the end"	default(100)
//	@Param			since		query	string	false	"Show logs since timestamp"
//	@Param			timestamps	query	bool	false	"Show timestamps"				default(false)
//	@Param			format		query	string	false	"Output format (text or json)"	default(text)
//	@Param			batched		query	bool	false	"Batch log messages"			default(false)
//	@Router			/api/environments/{id}/ws/swarm/services/{serviceId}/logs [get]
func (h *WebSocketHandler) ServiceLogs(c *echo.Context) error {
	serviceID := c.Param("serviceId")
	if strings.TrimSpace(serviceID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Service ID is required"})
	}

	params := parseLogStreamParamsInternal(c)
	h.serveLogStreamInternal(c, systemtypes.WSKindServiceLogs, serviceID, params, func(streamKey string, onEmpty func(*wsLogStream)) *wsLogStream {
		return h.startLogHubInternal(
			streamKey,
			serviceID,
			"service",
			params,
			h.swarmService.StreamServiceLogs,
			normalizeContainerLogMessageInternal,
			nil,
			onEmpty,
		)
	})
	return nil
}

// ContainerStats streams container stats over WebSocket.
//
//	@Summary		Get container stats via WebSocket
//	@Description	Stream container resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Router			/api/environments/{id}/ws/containers/{containerId}/stats [get]
func (h *WebSocketHandler) ContainerStats(c *echo.Context) error {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Container ID is required"})
	}

	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		slog.DebugContext(c.Request().Context(), "Failed to upgrade WebSocket for container stats", "containerID", containerID, "error", err)
		return nil
	}

	connID := h.wsMetrics.RegisterConnection(buildWSConnectionInfoInternal(c, systemtypes.WSKindContainerStats, containerID))
	onRemove := func() {
		h.wsMetrics.UnregisterConnection(connID)
	}

	// A reconnect can land exactly on the 5s idle-teardown boundary and load a
	// hub whose Run has already exited. Drop that hub and retry once so the
	// client gets a live producer instead of a silent socket.
	// WebSocket connections use context.Background() because they are long-lived and should not
	// be tied to the HTTP request context. Cleanup is handled by the shared hub when it idles.
	for attempt := range 2 {
		hub := h.getOrCreateContainerStatsHubInternal(containerID)
		if wshub.ServeClientWithOnRemove(context.Background(), hub, conn, onRemove) {
			return nil
		}
		h.containerStatsHubs.CompareAndDelete(containerID, hub)
		slog.DebugContext(c.Request().Context(), "container stats hub stopped before client registration; retrying",
			"containerID", containerID, "attempt", attempt+1)
	}

	slog.WarnContext(c.Request().Context(), "failed to register container stats client", "containerID", containerID)
	_ = conn.CloseNow()
	onRemove()
	return nil
}

func (h *WebSocketHandler) getOrCreateContainerStatsHubInternal(containerID string) *wshub.Hub {
	if existing, ok := h.containerStatsHubs.Load(containerID); ok {
		if hub, ok := existing.(*wshub.Hub); ok {
			return hub
		}
	}

	hub := wshub.NewHub(64)
	actual, loaded := h.containerStatsHubs.LoadOrStore(containerID, hub)
	if loaded {
		if existingHub, ok := actual.(*wshub.Hub); ok {
			return existingHub
		}
		// type assertion failure is impossible in practice, but avoid running
		// an unregistered hub if it somehow occurs
		return hub
	}

	h.runContainerStatsHubInternal(containerID, hub)
	return hub
}

// containerStatsErrorDrainGrace is how long a failed stats hub stays alive after
// broadcasting its error frame, so clients receive it before the hub shuts down.
const containerStatsErrorDrainGrace = 2 * time.Second

func (h *WebSocketHandler) runContainerStatsHubInternal(containerID string, hub *wshub.Hub) {
	ctx, cancel := context.WithCancel(context.Background())
	var cleanupTimer *time.Timer
	var cleanupTimerMu sync.Mutex

	hub.SetOnEmpty(func() {
		cleanupTimerMu.Lock()
		if cleanupTimer != nil {
			cleanupTimer.Stop()
		}
		var timer *time.Timer
		timer = time.AfterFunc(5*time.Second, func() {
			cleanupTimerMu.Lock()
			defer cleanupTimerMu.Unlock()
			if cleanupTimer != timer {
				return
			}
			if existing, ok := h.containerStatsHubs.Load(containerID); ok && existing == hub {
				h.containerStatsHubs.Delete(containerID)
			}
			slog.Debug("container stats hub idle, cleaning up upstream stream", "containerID", containerID)
			cleanupTimer = nil
			cancel()
		})
		cleanupTimer = timer
		cleanupTimerMu.Unlock()
	})
	hub.SetOnActive(func() {
		cleanupTimerMu.Lock()
		if cleanupTimer != nil {
			cleanupTimer.Stop()
			cleanupTimer = nil
		}
		cleanupTimerMu.Unlock()
	})

	go hub.Run(ctx)

	statsChan := make(chan any, 64)
	go func(ctx context.Context) {
		defer close(statsChan)

		err := h.containerService.StreamStats(ctx, containerID, statsChan)
		if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}

		// The producer died but the hub stayed cached, so clients kept an open
		// socket that would never emit another sample. Tell them why, then drop
		// the hub so the next connect rebuilds a producer instead of attaching
		// to this dead one.
		slog.Warn("container stats stream failed", "containerID", containerID, "error", err)
		if b, marshalErr := json.Marshal(map[string]any{
			"error":     "Failed to stream container stats: " + err.Error(),
			"timestamp": wshub.NowRFC3339(),
		}); marshalErr == nil {
			hub.Broadcast(b)
		}
		h.containerStatsHubs.CompareAndDelete(containerID, hub)

		// Cancelling immediately would race hub.Run between ctx.Done and the
		// queued error frame, dropping the very message clients need. Give the
		// broadcast a moment to drain, then tear the hub down.
		time.AfterFunc(containerStatsErrorDrainGrace, cancel)
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
}

// execProtoV1 opts an exec WebSocket client into the framed protocol: input
// keystrokes move from text to binary frames, and text frames carry JSON
// control messages (currently "ready" and "resize"). A client that omits
// ?proto=v1 stays in legacy mode — every frame, both directions, is raw
// bytes exactly as before resize support existed — so old clients and old
// agents (over an edge tunnel) keep working unchanged.
const execProtoV1 = "v1"

// execControlMaxBytes caps a text control frame before it is even handed to
// the JSON parser.
const execControlMaxBytes = 1024

// execResizeMinDim/execResizeMaxDim bound accepted TTY dimensions.
const (
	execResizeMinDim = 1
	execResizeMaxDim = 1000
)

// execControlMessage is the JSON shape exchanged over text frames once a
// connection is in framed mode. Not every field applies to every message
// type: "ready" only sets Protocol, "resize" only sets Cols/Rows.
type execControlMessage struct {
	Type     string `json:"type"`
	Cols     uint   `json:"cols,omitempty"`
	Rows     uint   `json:"rows,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

// execTerminalParams are the optional TTY-negotiation parameters a client
// may add to an exec WebSocket URL.
type execTerminalParams struct {
	proto string
	cols  uint
	rows  uint
}

func parseExecTerminalParamsInternal(c *echo.Context) execTerminalParams {
	return execTerminalParams{
		proto: queryParamWithDefaultInternal(c, "proto", ""),
		cols:  queryParamUintInternal(c, "cols", 0),
		rows:  queryParamUintInternal(c, "rows", 0),
	}
}

func queryParamUintInternal(c *echo.Context, key string, def uint) uint {
	v := c.QueryParam(key)
	if v == "" {
		return def
	}
	parsed, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return def
	}
	return uint(parsed)
}

// sendExecReadyInternal applies the client's initial TTY size (if supplied)
// and sends the one-time "ready" text frame that flips a proto=v1 client
// from legacy to framed mode. No-op when the client never negotiated v1, so
// a legacy client sees nothing it doesn't already expect.
func sendExecReadyInternal(ctx context.Context, conn *websocket.Conn, params execTerminalParams, resize func(cols, rows uint), logCtx ...any) {
	if params.proto != execProtoV1 {
		return
	}
	if resize != nil && params.cols > 0 && params.rows > 0 {
		resize(params.cols, params.rows)
	}
	ready, err := json.Marshal(execControlMessage{Type: "ready", Protocol: execProtoV1})
	if err != nil {
		return
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Write(wctx, websocket.MessageText, ready); err != nil {
		slog.Debug("Failed to send exec ready frame", append([]any{"error", err}, logCtx...)...)
	}
}

// execHandleControlMessageInternal parses a text frame as JSON. A frame
// that fails to parse is NOT a recognised control message (returns false)
// so the caller falls back to writing it to stdin verbatim — this is what
// keeps a raw client (e.g. wscat) that never speaks the control protocol
// working even after opting into proto=v1. A frame that does parse is
// always treated as consumed (returns true), whether or not its "type" is
// one this server acts on, since a framed-mode client never sends stdin
// bytes over a text frame.
func execHandleControlMessageInternal(data []byte, resize func(cols, rows uint)) bool {
	if len(data) > execControlMaxBytes {
		return false
	}
	var msg execControlMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return false
	}
	if msg.Type == "resize" && resize != nil &&
		msg.Cols >= execResizeMinDim && msg.Cols <= execResizeMaxDim &&
		msg.Rows >= execResizeMinDim && msg.Rows <= execResizeMaxDim {
		resize(msg.Cols, msg.Rows)
	}
	return true
}

// ContainerExec provides interactive terminal access to a container.
//
//	@Summary		Execute command in container via WebSocket
//	@Description	Interactive terminal access to a container over WebSocket
//	@Tags			WebSocket
//	@Param			id			path	string	true	"Environment ID"
//	@Param			containerId	path	string	true	"Container ID"
//	@Param			shell		query	string	false	"Shell to execute"	default(/bin/sh)
//	@Param			proto		query	string	false	"Set to v1 to enable TTY resize framing"
//	@Param			cols		query	int		false	"Initial terminal columns (proto=v1 only)"
//	@Param			rows		query	int		false	"Initial terminal rows (proto=v1 only)"
//	@Router			/api/environments/{id}/ws/containers/{containerId}/terminal [get]
func (h *WebSocketHandler) ContainerExec(c *echo.Context) error {
	containerID := c.Param("containerId")
	if strings.TrimSpace(containerID) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": "Container ID is required"})
	}

	shell := queryParamWithDefaultInternal(c, "shell", "/bin/sh")
	params := parseExecTerminalParamsInternal(c)

	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		return nil
	}
	connID := h.wsMetrics.RegisterConnection(buildWSConnectionInfoInternal(c, systemtypes.WSKindContainerExec, containerID))
	defer h.wsMetrics.UnregisterConnection(connID)
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close container exec websocket connection", "containerID", containerID, "error", err)
		}
	}()

	// Allow large terminal pastes; coder/websocket's default limit is 32KB.
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	go h.pingExecConnInternal(ctx, cancel, conn, 54*time.Second)

	h.runContainerExecInternal(ctx, cancel, conn, containerID, shell, params)
	return nil
}

// pingExecConnInternal keeps the exec websocket alive. Ping round-trips (the
// pong is serviced by the concurrent stdin reader) and is safe concurrently
// with the exec output writer. A failed ping means the client is gone, so it
// cancels the exec session — a silently-dead client would otherwise keep the
// session open until an output write fails.
func (h *WebSocketHandler) pingExecConnInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, period time.Duration) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func (h *WebSocketHandler) runContainerExecInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, containerID, shell string, params execTerminalParams) {
	// Create exec instance
	execID, err := h.containerService.CreateExec(ctx, containerID, []string{shell}, nil)
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error creating exec"))
		return
	}

	// Attach to exec
	execSession, err := h.containerService.AttachExec(ctx, containerID, execID)
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error attaching to exec"))
		return
	}
	cleanup := h.execCleanupFuncInternal(ctx, execSession, execID, containerID)
	defer cleanup()
	h.watchExecContextInternal(ctx, execID, containerID, cleanup)

	resize := func(cols, rows uint) {
		if err := h.containerService.ResizeExec(ctx, execID, cols, rows); err != nil {
			slog.Debug("Failed to resize exec TTY", "execID", execID, "containerID", containerID, "error", err)
		}
	}
	sendExecReadyInternal(ctx, conn, params, resize, "execID", execID, "containerID", containerID)

	done := make(chan struct{})
	go h.pipeExecOutputInternal(ctx, conn, execSession.Stdout(), execID, containerID, done)
	go h.pipeExecInputInternal(ctx, cancel, conn, execSession.Stdin(), execID, containerID, params.proto, resize)

	<-done
}

func (h *WebSocketHandler) writeExecErrorInternal(ctx context.Context, conn *websocket.Conn, err error) {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, []byte(err.Error()+"\r\n"))
}

func (h *WebSocketHandler) execCleanupFuncInternal(ctx context.Context, execSession *services.ExecSession, execID, containerID string) func() {
	return func() {
		slog.Debug("Cleaning up exec session", "execID", execID, "containerID", containerID, "contextErr", ctx.Err())
		// Cleanup must proceed even if parent ctx is canceled.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
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
			if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
				slog.Debug("Exec websocket write error", "execID", execID, "containerID", containerID, "error", err)
				return
			}
		}
	}
}

func (h *WebSocketHandler) pipeExecInputInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, stdin io.Writer, execID, containerID, proto string, resize func(cols, rows uint)) {
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			slog.Debug("Exec websocket read error", "execID", execID, "containerID", containerID, "error", err)
			cancel()
			return
		}

		if proto == execProtoV1 && msgType == websocket.MessageText && execHandleControlMessageInternal(data, resize) {
			continue
		}

		if _, err := stdin.Write(data); err != nil {
			slog.Debug("Exec stdin write error", "execID", execID, "containerID", containerID, "error", err)
			return
		}
	}
}

// HostTerminal provides an interactive root shell on the Docker host itself,
// via a privileged helper container that nsenters into PID 1's namespaces.
// Unlike ContainerExec, this is gated pre-upgrade by the hostTerminalEnabled
// setting (off by default) so a disabled host returns a real HTTP status
// instead of an opaque WebSocket close.
//
//	@Summary		Open a root shell on the Docker host via WebSocket
//	@Description	Interactive host-shell access over WebSocket. Disabled by default; requires the hostTerminalEnabled setting and the system:host-terminal permission.
//	@Tags			WebSocket
//	@Param			id		path	string	true	"Environment ID"
//	@Param			shell	query	string	false	"Shell to execute"	default(/bin/sh)
//	@Param			proto	query	string	false	"Set to v1 to enable TTY resize framing"
//	@Param			cols	query	int		false	"Initial terminal columns (proto=v1 only)"
//	@Param			rows	query	int		false	"Initial terminal rows (proto=v1 only)"
//	@Router			/api/environments/{id}/ws/system/terminal [get]
func (h *WebSocketHandler) HostTerminal(c *echo.Context) error {
	ctx := c.Request().Context()

	if h.hostShellService == nil || !h.hostShellService.Enabled(ctx) {
		return c.JSON(http.StatusForbidden, map[string]any{
			"success": false,
			"error":   "host terminal is disabled",
			"code":    "HOST_TERMINAL_DISABLED",
		})
	}

	shell := queryParamWithDefaultInternal(c, "shell", hostshell.DefaultShell)
	params := parseExecTerminalParamsInternal(c)
	actor := services.HostShellActor{
		UserID:    stringPtrOrNilInternal(getContextUserIDInternal(c)),
		Username:  stringPtrOrNilInternal(getContextUsernameInternal(c)),
		ClientIP:  c.RealIP(),
		UserAgent: c.Request().Header.Get("User-Agent"),
	}

	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		return nil
	}
	connID := h.wsMetrics.RegisterConnection(buildWSConnectionInfoInternal(c, systemtypes.WSKindHostTerminal, "host"))
	defer h.wsMetrics.UnregisterConnection(connID)
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close host terminal websocket connection", "error", err)
		}
	}()

	// Allow large terminal pastes; coder/websocket's default limit is 32KB.
	conn.SetReadLimit(1 << 20)

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go h.pingExecConnInternal(execCtx, cancel, conn, 54*time.Second)

	h.runHostTerminalInternal(execCtx, cancel, conn, shell, actor, params)
	return nil
}

func stringPtrOrNilInternal(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h *WebSocketHandler) runHostTerminalInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, shell string, actor services.HostShellActor, params execTerminalParams) {
	session, err := h.hostShellService.StartInteractive(ctx, services.StartInteractiveRequest{Shell: shell, Actor: actor})
	if err != nil {
		h.writeExecErrorInternal(ctx, conn, errors.WithMessage(err, "Error opening host terminal"))
		_ = conn.Close(hostTerminalCloseCodeInternal(err), err.Error())
		return
	}
	cleanup := h.hostShellCleanupFuncInternal(ctx, session)
	defer cleanup()
	h.watchHostShellContextInternal(ctx, session, cleanup)

	resize := func(cols, rows uint) {
		if err := session.Resize(ctx, cols, rows); err != nil {
			slog.Debug("Failed to resize host terminal TTY", "sessionID", session.ID(), "error", err)
		}
	}
	sendExecReadyInternal(ctx, conn, params, resize, "sessionID", session.ID())

	done := make(chan struct{})
	go h.pipeExecOutputInternal(ctx, conn, session.Stdout(), session.ID(), "host", done)
	go h.pipeExecInputInternal(ctx, cancel, conn, session.Stdin(), session.ID(), "host", params.proto, resize)

	<-done
}

// hostTerminalCloseCodeInternal maps a StartInteractive failure to a
// WebSocket close code so the client can distinguish "this host can never
// do this" (1008) from "try again later" (1013) from an infrastructure
// failure (1011).
func hostTerminalCloseCodeInternal(err error) websocket.StatusCode {
	switch {
	case errors.Is(err, services.ErrHostShellSessionLimit):
		return websocket.StatusTryAgainLater
	case errors.Is(err, services.ErrHostShellDisabled),
		errors.Is(err, hostshell.ErrInvalidShell),
		errors.Is(err, hostshell.ErrHostNotLinux),
		errors.Is(err, hostshell.ErrDockerDesktop),
		errors.Is(err, hostshell.ErrRootlessDaemon):
		return websocket.StatusPolicyViolation
	default:
		return websocket.StatusInternalError
	}
}

func (h *WebSocketHandler) hostShellCleanupFuncInternal(ctx context.Context, session *services.HostShellSession) func() {
	return func() {
		slog.Debug("Cleaning up host shell session", "sessionID", session.ID(), "contextErr", ctx.Err())
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		session.Close(cleanupCtx, "client_closed") //nolint:contextcheck
	}
}

func (h *WebSocketHandler) watchHostShellContextInternal(ctx context.Context, session *services.HostShellSession, cleanup func()) {
	go func() {
		<-ctx.Done()
		slog.Debug("Host shell context cancelled", "sessionID", session.ID())
		cleanup()
	}()
}

// ============================================================================
// System WebSocket Endpoints
// ============================================================================

// checkRateLimitInternal checks and applies rate limiting for WebSocket connections.
// Returns the counter and whether the connection should be allowed.
func (h *WebSocketHandler) checkRateLimitInternal(clientIP string) (*int32, bool) {
	connCount, _ := h.activeConnections.LoadOrStore(clientIP, new(int32))
	count, ok := connCount.(*int32)
	if !ok {
		return nil, false
	}

	currentCount := atomic.AddInt32(count, 1)
	if currentCount > 5 {
		atomic.AddInt32(count, -1)
		return nil, false
	}
	return count, true
}

// releaseRateLimitInternal decrements the connection counter and cleans up if needed.
func (h *WebSocketHandler) releaseRateLimitInternal(clientIP string, count *int32) {
	newCount := atomic.AddInt32(count, -1)
	if newCount <= 0 {
		// CompareAndDelete, not Delete: a plain delete removed whatever counter
		// was stored for this IP, which after a racing reconnect is a live one —
		// wiping its count and letting that IP exceed the limit.
		h.activeConnections.CompareAndDelete(clientIP, count)
	}
}

func (h *WebSocketHandler) acquireSystemStatsSamplerInternal(ctx context.Context) bool {
	h.systemStatsSampler.lifecycleMu.Lock()

	h.systemStatsSampler.clients++
	if h.systemStatsSampler.running {
		ready := h.systemStatsSampler.ready
		h.systemStatsSampler.lifecycleMu.Unlock()
		return waitForSystemStatsSamplerReadyInternal(ctx, ready)
	}

	samplerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	ready := make(chan struct{})
	h.systemStatsSampler.cancel = cancel
	h.systemStatsSampler.ready = ready
	h.systemStatsSampler.running = true
	h.systemStatsSampler.lifecycleMu.Unlock()

	go func() {
		closeReady := sync.OnceFunc(func() {
			close(ready)
		})
		if !h.initializeCPUCacheCtx(samplerCtx) {
			closeReady()
			return
		}
		if samplerCtx.Err() != nil {
			closeReady()
			return
		}

		h.storeSystemStatsSnapshotInternal(h.collectSystemStatsSnapshotInternal(samplerCtx))
		closeReady()
		if samplerCtx.Err() != nil {
			return
		}

		h.runSystemStatsSamplerInternal(samplerCtx)
	}()

	return waitForSystemStatsSamplerReadyInternal(ctx, ready)
}

func waitForSystemStatsSamplerReadyInternal(ctx context.Context, ready <-chan struct{}) bool {
	if ready == nil {
		return true
	}

	select {
	case <-ctx.Done():
		return false
	case <-ready:
		return ctx.Err() == nil
	}
}

func (h *WebSocketHandler) releaseSystemStatsSamplerInternal() {
	var cancel context.CancelFunc

	h.systemStatsSampler.lifecycleMu.Lock()
	if h.systemStatsSampler.clients > 0 {
		h.systemStatsSampler.clients--
	}
	if h.systemStatsSampler.clients == 0 && h.systemStatsSampler.running {
		cancel = h.systemStatsSampler.cancel
		h.systemStatsSampler.cancel = nil
		h.systemStatsSampler.ready = nil
		h.systemStatsSampler.running = false
	}
	h.systemStatsSampler.lifecycleMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (h *WebSocketHandler) runSystemStatsSamplerInternal(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.updateCPUCacheInternal(0)
			h.storeSystemStatsSnapshotInternal(h.collectSystemStatsSnapshotInternal(ctx))
		}
	}
}

func (h *WebSocketHandler) storeSystemStatsSnapshotInternal(stats systemtypes.SystemStats) {
	h.systemStatsSampler.stateMu.Lock()
	h.systemStatsSampler.latest = stats
	h.systemStatsSampler.timestamp = time.Now()
	h.systemStatsSampler.stateMu.Unlock()
}

func (h *WebSocketHandler) latestSystemStatsSnapshotInternal() systemtypes.SystemStats {
	h.systemStatsSampler.stateMu.RLock()
	stats := h.systemStatsSampler.latest
	h.systemStatsSampler.stateMu.RUnlock()
	return stats
}

func (h *WebSocketHandler) collectSystemStatsSnapshotInternal(ctx context.Context) systemtypes.SystemStats {
	if h.systemStatsCollector != nil {
		return h.systemStatsCollector(ctx)
	}
	return h.collectSystemStats(ctx)
}

// collectSystemStats gathers all system statistics.
func (h *WebSocketHandler) collectSystemStats(ctx context.Context) systemtypes.SystemStats {
	h.cpuCache.RLock()
	cpuUsage := h.cpuCache.value
	h.cpuCache.RUnlock()

	cpuCount := h.getCPUCount()
	memUsed, memTotal := h.getMemoryInfo()
	cpuCount, memUsed, memTotal = h.applyCgroupLimits(cpuCount, memUsed, memTotal)
	diskUsed, diskTotal := h.getDiskInfo(ctx)
	hostname := h.getHostname()
	gpuStats, gpuCount := h.getGPUInfo(ctx)

	return systemtypes.SystemStats{
		CPUUsage:     cpuUsage,
		MemoryUsage:  memUsed,
		MemoryTotal:  memTotal,
		DiskUsage:    diskUsed,
		DiskTotal:    diskTotal,
		CPUCount:     cpuCount,
		Architecture: runtime.GOARCH,
		Platform:     runtime.GOOS,
		Hostname:     hostname,
		GPUCount:     gpuCount,
		GPUs:         gpuStats,
	}
}

// getCPUCount returns the number of CPUs.
func (h *WebSocketHandler) getCPUCount() int {
	h.initSystemStaticInfoInternal()
	return h.systemStaticInfo.cpuCount
}

func (h *WebSocketHandler) initSystemStaticInfoInternal() {
	h.systemStaticInfo.once.Do(func() {
		cpuCount, err := cpu.Counts(true)
		if err != nil {
			cpuCount = runtime.NumCPU()
		}

		hostInfo, _ := host.Info()
		hostname := ""
		if hostInfo != nil {
			hostname = hostInfo.Hostname
		}

		h.systemStaticInfo.cpuCount = cpuCount
		h.systemStaticInfo.hostname = hostname
	})
}

// getMemoryInfo returns memory usage and total.
func (h *WebSocketHandler) getMemoryInfo() (uint64, uint64) {
	memInfo, _ := mem.VirtualMemory()
	if memInfo == nil {
		return 0, 0
	}
	// gopsutil counts ZFS ARC as used memory (the kernel excludes it from
	// MemAvailable). Treat the reclaimable portion as cache, matching
	// btop/htop, so the dashboard does not over-report usage on ZFS hosts.
	used := memInfo.Used
	if arc := cgroup.ZFSARCReclaimable(); arc > 0 {
		used -= min(used, arc)
	}
	return used, memInfo.Total
}

// applyCgroupLimits applies cgroup limits when running in an LXC (or similar)
// container where the limits represent the real hardware budget.
//
// It is intentionally a no-op inside Docker: Docker's --cpus / --memory flags
// set artificial cgroup constraints that are unrelated to the host totals we
// want to display. gopsutil already reads the correct host values there (via
// the bind-mounted /proc). Applying cgroup limits on top would produce the
// "#2343 regression" where the dashboard shows "512 MB RAM" while the host
// has 32 GB (#1110).
//
// In LXC the situation is the opposite: gopsutil reads the host's /proc
// (which shows the physical machine's RAM/CPU) rather than the slice of
// resources actually allocated to the LXC guest. The cgroup limits ARE the
// correct numbers to show.
func (h *WebSocketHandler) applyCgroupLimits(cpuCount int, memUsed, memTotal uint64) (int, uint64, uint64) {
	if cgroup.IsDockerContainer() {
		return cpuCount, memUsed, memTotal
	}
	cgroupLimits := h.getCachedCgroupLimitsInternal()
	if cgroupLimits == nil {
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
	h.initSystemStaticInfoInternal()
	return h.systemStaticInfo.hostname
}

// getGPUInfo returns GPU statistics if monitoring is enabled.
func (h *WebSocketHandler) getGPUInfo(ctx context.Context) ([]systemtypes.GPUStats, int) {
	if h.gpuMonitor == nil || !h.gpuMonitor.Enabled() {
		return nil, 0
	}
	gpuData, err := h.gpuMonitor.Stats(ctx)
	if err != nil {
		return nil, 0
	}
	return gpuData, len(gpuData)
}

// initializeCPUCacheCtx performs initial CPU sampling and returns early if the sampler is canceled.
func (h *WebSocketHandler) initializeCPUCacheCtx(ctx context.Context) bool {
	result := make(chan float64, 1)

	go func() {
		if val, ok := h.readCPUUsageInternal(time.Second); ok {
			result <- val
		}
		close(result)
	}()

	select {
	case <-ctx.Done():
		return false
	case val, ok := <-result:
		if !ok || ctx.Err() != nil {
			return false
		}
		h.storeCPUCacheValueInternal(val)
		return true
	}
}

func (h *WebSocketHandler) updateCPUCacheInternal(interval time.Duration) {
	if val, ok := h.readCPUUsageInternal(interval); ok {
		h.storeCPUCacheValueInternal(val)
	}
}

func (h *WebSocketHandler) readCPUUsageInternal(interval time.Duration) (float64, bool) {
	if h.cpuUsageReader != nil {
		return h.cpuUsageReader(interval)
	}

	return defaultReadCPUUsageInternal(interval)
}

var defaultReadCPUUsageInternal = func(interval time.Duration) (float64, bool) {
	if vals, err := cpu.Percent(interval, false); err == nil && len(vals) > 0 {
		return vals[0], true
	}

	return 0, false
}

func (h *WebSocketHandler) storeCPUCacheValueInternal(value float64) {
	h.cpuCache.Lock()
	h.cpuCache.value = value
	h.cpuCache.timestamp = time.Now()
	h.cpuCache.Unlock()
}

func (h *WebSocketHandler) getCachedCgroupLimitsInternal() *cgroup.Limits {
	if h.cgroupCache == nil {
		return nil
	}
	return h.cgroupCache.Get()
}

// SystemStats streams system stats over WebSocket.
//
//	@Summary		Get system stats via WebSocket
//	@Description	Stream system resource statistics over WebSocket connection
//	@Tags			WebSocket
//	@Param			id	path	string	true	"Environment ID"
//	@Router			/api/environments/{id}/ws/system/stats [get]
func (h *WebSocketHandler) SystemStats(c *echo.Context) error {
	clientIP := c.RealIP()

	count, allowed := h.checkRateLimitInternal(clientIP)
	if !allowed {
		return c.JSON(http.StatusTooManyRequests, map[string]any{
			"success": false,
			"error":   "Too many concurrent stats connections from this IP",
		})
	}
	defer h.releaseRateLimitInternal(clientIP, count)

	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		return nil
	}
	connID := h.wsMetrics.RegisterConnection(buildWSConnectionInfoInternal(c, systemtypes.WSKindSystemStats, ""))
	defer h.wsMetrics.UnregisterConnection(connID)
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close system stats websocket connection", "clientIP", clientIP, "error", err)
		}
	}()

	interval, _ := httputil.GetIntQueryParam(c.Request(), "interval", false)
	if interval <= 0 {
		interval = 2
	}

	const statsPingPeriod = 54 * time.Second

	conn.SetReadLimit(512)

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	pingTicker := time.NewTicker(statsPingPeriod)
	defer pingTicker.Stop()

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()
	if !h.acquireSystemStatsSamplerInternal(ctx) {
		h.releaseSystemStatsSamplerInternal()
		return nil
	}
	defer h.releaseSystemStatsSamplerInternal()

	go h.readSystemStatsPumpInternal(ctx, cancel, conn)

	send := func() error {
		stats := h.latestSystemStatsSnapshotInternal()
		b, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, b)
	}

	if err := send(); err != nil {
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := send(); err != nil {
				return nil
			}
		case <-pingTicker.C:
			// Ping round-trips; the pong is serviced by readSystemStatsPumpInternal.
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			pcancel()
			if err != nil {
				return nil
			}
		}
	}
}

// readSystemStatsPumpInternal is the single reader for the SystemStats websocket.
// Do not add additional readers for this connection.
func (h *WebSocketHandler) readSystemStatsPumpInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			cancel()
			return
		}
	}
}

func (h *WebSocketHandler) getDiskUsagePath(ctx context.Context) string {
	if h.diskUsagePathCache == nil {
		h.diskUsagePathCache = hot.NewHotCache[struct{}, string](hot.LRU, 1).
			WithTTL(5 * time.Minute).
			Build()
	}
	path, found, err := h.diskUsagePathCache.GetWithLoaders(struct{}{}, func(_ []struct{}) (map[struct{}]string, error) {
		path := "/"
		if h.systemService != nil {
			path = h.systemService.GetDiskUsagePath(ctx)
		}
		return map[struct{}]string{{}: path}, nil
	})
	if err != nil || !found {
		return "/"
	}
	return path
}
