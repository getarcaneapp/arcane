package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/samber/hot"
	"go.getarcane.app/streams/agg"
	"go.getarcane.app/sys/cgroup"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane"
	systemlib "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/system"
	wshub "github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/ws"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/tracing"
)

var (
	defaultWebSocketMetrics     = wshub.NewWebSocketMetrics()
	observeWebSocketMetricsOnce sync.Once
)

// WebSocketHandler consolidates all WebSocket and streaming endpoints.
// REST endpoints are handled by Huma handlers.
type WebSocketHandler struct {
	// ctx ends after open connections were closed with StatusGoingAway on app shutdown.
	ctx                 context.Context
	conns               sync.Map
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
		latest            concurrency.Snapshot[types.SystemStats]
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
	containersStatsHub *agg.Hub[containersStatsResult]
	cgroupCache        *cgroup.Cache
	gpuMonitor         atomic.Pointer[systemlib.GPUMonitor]

	diskUsagePathCache   *hot.HotCache[struct{}, string]
	projectLogStreamer   func(ctx context.Context, projectID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error
	containerLogStreamer func(ctx context.Context, containerID string, logsChan chan<- string, follow bool, tail, since string, timestamps bool) error
	systemStatsCollector func(ctx context.Context) types.SystemStats
	cpuUsageReader       func(interval time.Duration) (float64, bool)
}

func NewWebSocketHandler(
	appCtx context.Context,
	group *echo.Group,
	projectService *project.ProjectService,
	containerService *container.ContainerService,
	swarmService *swarm.SwarmService,
	systemService *system.SystemService,
	diagnosticsService *diagnostics.DiagnosticsService,
	authMiddleware *auth.AuthMiddleware,
	settingsService *settings.SettingsService,
	cfg *config.Config,
) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(appCtx))
	handler := &WebSocketHandler{
		ctx:                ctx,
		projectService:     projectService,
		containerService:   containerService,
		swarmService:       swarmService,
		systemService:      systemService,
		diagnosticsService: diagnosticsService,
		logStreams:         make(map[string]*wsLogStream),
		containersStatsHub: agg.NewHub[containersStatsResult](),
		cgroupCache:        cgroup.NewCache(cgroupCacheTTL),
		diskUsagePathCache: hot.NewHotCache[struct{}, string](hot.LRU, 1).
			WithTTL(5 * time.Minute).
			Build(),
		checkWSOrigin: httpx.ValidateWebSocketOrigin(cfg.GetAppURL()),
	}
	newGPUMonitor := func() *systemlib.GPUMonitor {
		return systemlib.NewGPUMonitor(settingsService.GetBoolSetting(ctx, "gpuMonitoringEnabled"), settingsService.GetStringSetting(ctx, "gpuType"))
	}
	handler.gpuMonitor.Store(newGPUMonitor())
	context.AfterFunc(ctx, settingsService.SubscribeSettingsChanges([]string{"gpuMonitoringEnabled", "gpuType"}, func([]libarcane.SettingUpdate) {
		handler.gpuMonitor.Store(newGPUMonitor())
	}))
	// Send the close frames before cancelling hubs, whose teardown closes connections without a handshake.
	context.AfterFunc(appCtx, func() {
		var wg sync.WaitGroup
		handler.conns.Range(func(key, _ any) bool {
			wg.Go(func() {
				_ = key.(*websocket.Conn).Close(websocket.StatusGoingAway, "server shutting down")
			})
			return true
		})
		wg.Wait()
		cancel()
	})
	observeWebSocketMetricsOnce.Do(func() {
		if err := defaultWebSocketMetrics.ObserveConnections(otel.Meter(tracing.InstrumentationName)); err != nil {
			otel.Handle(err)
		}
	})
	wsGroup := group.Group("/environments/:id/ws", authMiddleware.WithAdminNotRequired().Add())
	for _, r := range handler.proxiedRoutes() {
		wsGroup.GET(r.path, r.handler, middleware.RequireEchoPermission(r.perm))
	}
	handler.registerDiagnosticsRoutes(group, authMiddleware)
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
	info := types.WebSocketConnectionInfo{
		Kind:       kind,
		EnvID:      c.Param("id"),
		ResourceID: resourceID,
		ClientIP:   c.RealIP(),
		UserID:     userID,
		UserAgent:  req.Header.Get("User-Agent"),
	}
	connID := defaultWebSocketMetrics.RegisterConnection(info)
	h.conns.Store(conn, struct{}{})
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
		h.conns.Delete(conn)
		defaultWebSocketMetrics.UnregisterConnection(connID)
		span.End()
	}, true
}
