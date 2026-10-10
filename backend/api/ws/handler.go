package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/labstack/echo/v5"
	"github.com/samber/hot"
	"go.getarcane.app/sys/cgroup"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

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
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

var (
	defaultWebSocketMetrics     = wshub.NewWebSocketMetrics()
	observeWebSocketMetricsOnce sync.Once
)

// WebSocketHandler consolidates all WebSocket and streaming endpoints.
// REST endpoints are handled by Huma handlers.
type WebSocketHandler struct {
	projectService      *project.ProjectService
	containerService    *container.ContainerService
	swarmService        *swarm.SwarmService
	systemService       *system.SystemService
	diagnosticsService  *diagnostics.DiagnosticsService
	checkWSOrigin       func(*http.Request) bool
	activeConnectionsMu sync.Mutex
	activeConnections   map[string]int
	logStreamsMu        sync.Mutex
	logStreams          map[string]*wsLogStream
	cpuCache            concurrency.Snapshot[float64]
	systemStaticInfo    struct {
		once     sync.Once
		cpuCount int
		hostname string
	}
	systemStatsSampler struct {
		latest            concurrency.Snapshot[systemtypes.SystemStats]
		lifecycleMu       sync.Mutex
		clients           int
		intervals         map[time.Duration]int
		effectiveInterval atomic.Int64
		cancel            context.CancelFunc
		ready             chan struct{}
		wake              chan struct{}
		running           bool
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
		logStreams:         make(map[string]*wsLogStream),
		cgroupCache:        cgroup.NewCache(cgroupCacheTTL),
		gpuMonitor:         systemlib.NewGPUMonitor(cfg.GPUMonitoringEnabled, cfg.GPUType),
		diskUsagePathCache: hot.NewHotCache[struct{}, string](hot.LRU, 1).
			WithTTL(5 * time.Minute).
			Build(),
		checkWSOrigin: httpx.ValidateWebSocketOrigin(cfg.GetAppURL()),
	}
	observeWebSocketMetricsOnce.Do(func() {
		if err := defaultWebSocketMetrics.ObserveConnections(otel.Meter(tracing.InstrumentationName)); err != nil {
			otel.Handle(err)
		}
	})
	wsGroup := group.Group("/environments/:id/ws", authMiddleware.WithAdminNotRequired().Add())
	for _, r := range handler.proxiedRoutes() {
		wsGroup.GET(r.path, r.handler, middleware.RequireEchoPermission(r.perm))
	}
	handler.registerDiagnosticsRoutesInternal(group, authMiddleware)
}

// acceptWS upgrades the request to a WebSocket, registers it with the metrics tracker, and starts its lifetime span.
// The returned unregister func must be called exactly once when the connection ends.
func (h *WebSocketHandler) acceptWS(c *echo.Context, kind, resourceID string) (*websocket.Conn, func(), bool) {
	req := c.Request()
	conn, err := wshub.Accept(c.Response(), req, h.checkWSOrigin)
	if err != nil {
		slog.DebugContext(req.Context(), "websocket accept failed", "kind", kind, "resourceId", resourceID, "error", err)
		return nil, nil, false
	}
	userID, _ := c.Get("userID").(string)
	info := systemtypes.WebSocketConnectionInfo{
		Kind:       kind,
		EnvID:      c.Param("id"),
		ResourceID: resourceID,
		ClientIP:   c.RealIP(),
		UserID:     userID,
		UserAgent:  req.Header.Get("User-Agent"),
	}
	connID := defaultWebSocketMetrics.RegisterConnection(info)
	// The HTTP tracing middleware skips upgrades, so this is a root span unless the client sent a traceparent.
	parent := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))
	_, span := otel.Tracer(tracing.InstrumentationName).Start(parent, "websocket "+kind,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("arcane.websocket.kind", kind),
			attribute.String("arcane.websocket.connection_id", connID),
			attribute.String("arcane.environment.id", info.EnvID),
			attribute.String("arcane.websocket.resource_id", resourceID),
			attribute.String("http.route", c.Path()),
			attribute.String("client.address", info.ClientIP),
			attribute.String("user.id", userID),
			attribute.String("user_agent.original", info.UserAgent),
		))
	return conn, func() {
		defaultWebSocketMetrics.UnregisterConnection(connID)
		span.End()
	}, true
}

// keepWSConnAlive pings the peer every period and cancels the connection when a ping fails.
// The pong must be serviced by the connection's concurrent reader.
func keepWSConnAlive(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, period time.Duration) {
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
