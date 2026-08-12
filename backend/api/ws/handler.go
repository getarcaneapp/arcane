package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"
	"github.com/samber/hot"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	systemlib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/system"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	httputil "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/sys/cgroup"
)

var defaultWebSocketMetrics = wshub.NewWebSocketMetrics()

// ============================================================================
// WebSocket Handler
// ============================================================================

// WebSocketHandler consolidates all WebSocket and streaming endpoints.
// REST endpoints are handled by Huma handlers.
type WebSocketHandler struct {
	projectService     *project.ProjectService
	containerService   *container.ContainerService
	swarmService       *swarm.SwarmService
	systemService      *system.SystemService
	diagnosticsService *diagnostics.DiagnosticsService
	checkWSOrigin      func(*http.Request) bool
	wsMetrics          *wshub.WebSocketMetrics
	activeConnections  sync.Map
	logStreamsMu       sync.Mutex
	logStreams         map[string]*wsLogStream
	cpuCache           actors.Snapshot[float64]
	systemStaticInfo   struct {
		once     sync.Once
		cpuCount int
		hostname string
	}
	systemStatsSampler struct {
		latest      actors.Snapshot[systemtypes.SystemStats]
		lifecycleMu sync.Mutex
		clients     int
		// intervals counts subscribers per requested interval; the sampler
		// ticks at the smallest one (in effectiveInterval) rather than a
		// fixed 1Hz, so samples are never produced faster than the fastest
		// consumer reads them.
		intervals         map[time.Duration]int
		effectiveInterval atomic.Int64
		cancel            context.CancelFunc
		ready             chan struct{}
		// wake nudges the running sampler when effectiveInterval changes,
		// so a newly joined faster subscriber doesn't wait out a slower
		// tick already in flight.
		wake    chan struct{}
		running bool
	}
	containerStatsHubs sync.Map
	cgroupCache        *cgroup.Cache
	gpuMonitor         *systemlib.GPUMonitor

	diskUsagePathCache   *hot.HotCache[struct{}, string]
	projectLogStreamer   func(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error
	containerLogStreamer func(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error
	systemStatsCollector func(ctx context.Context) systemtypes.SystemStats
	cpuUsageReader       func(interval time.Duration) (float64, bool)
}

func NewWebSocketHandler(
	group *echo.Group,
	projectService *project.ProjectService,
	containerService *container.ContainerService,
	swarmService *swarm.SwarmService,
	systemService *system.SystemService,
	diagnosticsService *diagnostics.DiagnosticsService,
	authMiddleware *auth.AuthMiddleware,
	cfg *config.Config,
) {
	handler := &WebSocketHandler{
		projectService:     projectService,
		containerService:   containerService,
		swarmService:       swarmService,
		systemService:      systemService,
		diagnosticsService: diagnosticsService,
		wsMetrics:          defaultWebSocketMetrics,
		logStreams:         make(map[string]*wsLogStream),
		cgroupCache:        cgroup.NewCache(cgroupCacheTTL),
		gpuMonitor:         systemlib.NewGPUMonitor(cfg.GPUMonitoringEnabled, cfg.GPUType),
		diskUsagePathCache: hot.NewHotCache[struct{}, string](hot.LRU, 1).
			WithTTL(5 * time.Minute).
			Build(),
		checkWSOrigin: httputil.ValidateWebSocketOrigin(cfg.GetAppURL()),
	}
	wsGroup := group.Group("/environments/:id/ws", authMiddleware.WithAdminNotRequired().Add())
	for _, r := range handler.proxiedRoutes() {
		wsGroup.GET(r.path, r.handler, middleware.RequireEchoPermission(r.perm))
	}
	handler.registerDiagnosticsRoutesInternal(group, authMiddleware)
}

func buildWSConnectionInfoInternal(c *echo.Context, kind, resourceID string) systemtypes.WebSocketConnectionInfo {
	userID, _ := c.Get("userID").(string)
	return systemtypes.WebSocketConnectionInfo{
		Kind:       kind,
		EnvID:      c.Param("id"),
		ResourceID: resourceID,
		ClientIP:   c.RealIP(),
		UserID:     userID,
		UserAgent:  c.Request().Header.Get("User-Agent"),
	}
}

// acceptWSInternal upgrades the request to a WebSocket and registers it with
// the metrics tracker. The returned unregister func must be called exactly
// once when the connection ends.
func (h *WebSocketHandler) acceptWSInternal(c *echo.Context, kind, resourceID string) (*websocket.Conn, func(), bool) {
	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		slog.DebugContext(c.Request().Context(), "websocket accept failed", "kind", kind, "resourceID", resourceID, "error", err)
		return nil, nil, false
	}
	connID := h.wsMetrics.RegisterConnection(buildWSConnectionInfoInternal(c, kind, resourceID))
	return conn, func() { h.wsMetrics.UnregisterConnection(connID) }, true
}

// wsErrorJSONInternal replies with the {"success":false,"error":...} body
// shared by every WS endpoint's pre-upgrade error path.
func wsErrorJSONInternal(c *echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]any{"success": false, "error": msg})
}

// keepWSConnAliveInternal pings the peer every period. Ping round-trips (the
// pong must be serviced by the connection's concurrent reader) and is safe
// alongside a concurrent writer. A failed ping means the client is gone, so
// it cancels the connection's context — a silently-dead client would
// otherwise keep the session open until a write fails.
func keepWSConnAliveInternal(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, period time.Duration) {
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
