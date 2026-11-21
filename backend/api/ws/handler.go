package ws

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"kit.getarcane.app/cache"
	"kit.getarcane.app/concurrency"
	"kit.getarcane.app/docker/streams/agg"
	"kit.getarcane.app/sys/cgroup"

	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/diagnostics"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/logging"
	systemlib "getarcane.dev/arcane/v3/pkg/system"
	wshub "getarcane.dev/arcane/v3/pkg/ws"
	"getarcane.dev/types/v3"
)

type (
	WebSocketHandler struct {
		// ctx ends after open connections were closed with StatusGoingAway on app shutdown.
		ctx                context.Context
		projectService     *project.ProjectService
		containerService   *container.ContainerService
		swarmService       *swarm.SwarmService
		systemService      *system.SystemService
		settingsService    *settings.SettingsService
		diagnosticsService *diagnostics.DiagnosticsService
		checkWSOrigin      func(*http.Request) bool
		activeConnections  map[string]int
		logStreams         map[string]*wsLogStream
		cpuCache           concurrency.Snapshot[float64]
		containersStatsHub *agg.Hub[containersStatsResult]
		cgroupCache        *cgroup.Cache
		gpuMonitor         atomic.Pointer[systemlib.GPUMonitor]

		diskUsagePathCache   *cache.Cache[struct{}, string]
		projectLogStreamer   func(ctx context.Context, projectID string, logsChan chan<- string, opts types.LogStreamOptions) error
		containerLogStreamer func(ctx context.Context, containerID string, logsChan chan<- string, opts types.LogStreamOptions) error
		systemStatsCollector func(ctx context.Context) types.SystemStats
		cpuUsageReader       func(interval time.Duration) (float64, bool)
		systemStatsSampler   systemStatsSampler
		conns                sync.Map
		containerStatsHubs   sync.Map
		systemStaticInfo     struct {
			hostname string
			cpuCount int
			once     sync.Once
		}
		activeConnectionsMu sync.Mutex
		logStreamsMu        sync.Mutex
	}
	systemStatsSampler struct {
		latest            concurrency.Snapshot[types.SystemStats]
		intervals         map[time.Duration]int
		cancel            context.CancelFunc
		ready             chan struct{}
		wake              chan struct{}
		clients           int
		effectiveInterval atomic.Int64
		lifecycleMu       sync.Mutex
		running           bool
	}
	HandlerParams struct {
		AppCtx             context.Context
		Group              *echo.Group
		ProjectService     *project.ProjectService
		ContainerService   *container.ContainerService
		SwarmService       *swarm.SwarmService
		SystemService      *system.SystemService
		DiagnosticsService *diagnostics.DiagnosticsService
		AuthMiddleware     *auth.AuthMiddleware
		SettingsService    *settings.SettingsService
		Config             *config.Config
	}
)

var (
	defaultWebSocketMetrics     = wshub.NewWebSocketMetrics()
	observeWebSocketMetricsOnce sync.Once
)

func NewWebSocketHandler(p HandlerParams) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(p.AppCtx))
	handler := &WebSocketHandler{
		ctx:                ctx,
		projectService:     p.ProjectService,
		containerService:   p.ContainerService,
		swarmService:       p.SwarmService,
		systemService:      p.SystemService,
		settingsService:    p.SettingsService,
		diagnosticsService: p.DiagnosticsService,
		activeConnections:  make(map[string]int),
		logStreams:         make(map[string]*wsLogStream),
		systemStatsSampler: systemStatsSampler{intervals: make(map[time.Duration]int)},
		containersStatsHub: &agg.Hub[containersStatsResult]{},
		cgroupCache:        cgroup.NewCache(cgroup.CacheConfig{TTL: cgroupCacheTTL}),
		diskUsagePathCache: &cache.Cache[struct{}, string]{
			Capacity: 1,
			TTL:      5 * time.Minute,
		},
		cpuUsageReader: readHostCPUUsage,
		checkWSOrigin:  httpx.ValidateWebSocketOrigin(p.Config.GetAppURL()),
	}
	newGPUMonitor := func() *systemlib.GPUMonitor {
		return systemlib.NewGPUMonitor(p.SettingsService.GetBoolSetting(ctx, "gpuMonitoringEnabled"), p.SettingsService.GetStringSetting(ctx, "gpuType"))
	}
	handler.gpuMonitor.Store(newGPUMonitor())
	context.AfterFunc(ctx, p.SettingsService.SubscribeSettingsChanges([]string{"gpuMonitoringEnabled", "gpuType"}, func([]types.SettingUpdate) {
		handler.gpuMonitor.Store(newGPUMonitor())
	}))
	// Send the close frames before cancelling hubs, whose teardown closes connections without a handshake.
	context.AfterFunc(p.AppCtx, func() {
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
		if err := defaultWebSocketMetrics.ObserveConnections(otel.Meter(logging.InstrumentationName)); err != nil {
			otel.Handle(err)
		}
	})
	middleware.RegisterEchoRoutes(p.Group, slices.Concat(handler.proxiedRoutes(), handler.diagnosticsRoutes()), p.AuthMiddleware.WithAdminNotRequired().Add())
}

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
	_, span := otel.Tracer(logging.InstrumentationName).Start(parent, "websocket "+kind,
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

func requireWSParam(c *echo.Context, param, label string) (string, bool, error) {
	value := c.Param(param)
	if strings.TrimSpace(value) == "" {
		return "", false, c.JSON(http.StatusBadRequest, map[string]any{"success": false, "error": label + " is required"})
	}
	return value, true, nil
}

func writeWSJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(wctx, websocket.MessageText, b)
}
