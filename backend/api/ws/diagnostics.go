package ws

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/coder/websocket"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/streams/logs"

	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
)

const (
	// diagnosticsStreamInterval is how often the live diagnostics stream pushes a snapshot.
	diagnosticsStreamInterval = 2 * time.Second
	// actorDiagnosticsStreamInterval is slower because each push queries every environment.
	actorDiagnosticsStreamInterval = 10 * time.Second

	// Keepalive/liveness bounds for the diagnostics sockets, mirroring the system
	// stats stream. Without them a silently dead peer wedges the writer forever,
	// which also strands the writer's deferred cleanup (log subscription, conn).
	diagnosticsReadLimit  = 512
	diagnosticsWriteWait  = 10 * time.Second
	diagnosticsPingWait   = 10 * time.Second
	diagnosticsPingPeriod = 54 * time.Second
)

// buildDiagnosticsInternal assembles a snapshot: runtime/memory/GC from the
// DiagnosticsService plus this package's WebSocket metrics and worker-goroutine count.
func buildDiagnosticsInternal(diag *diagnostics.DiagnosticsService) systemtypes.Diagnostics {
	d := systemtypes.Diagnostics{Timestamp: time.Now().UTC()}
	if diag != nil {
		d.Runtime, d.Memory, d.GC = diag.Collect()
	}
	d.Runtime.WSWorkerGoroutines = wshub.CountWorkerGoroutines()
	d.WebSocket = systemtypes.WebSocketDiagnostics{
		Snapshot:    defaultWebSocketMetrics.Snapshot(),
		Connections: defaultWebSocketMetrics.Connections(),
	}
	return d
}

// registerDiagnosticsRoutesInternal wires the global diagnostics WebSocket streams and the
// net/http/pprof debug endpoints. Streams require the diagnostics permission;
// pprof keeps the stricter admin-required gate. Called from NewWebSocketHandler.
func (h *WebSocketHandler) registerDiagnosticsRoutesInternal(group *echo.Group, authMiddleware *auth.AuthMiddleware) {
	diag := group.Group("/diagnostics",
		authMiddleware.WithAdminNotRequired().Add(),
		middleware.RequireEchoPermission(authz.PermDiagnosticsRead),
	)
	diag.GET("/stream", h.DiagnosticsStream)
	diag.GET("/actors/stream", h.ActorDiagnosticsStream)
	diag.GET("/logs/stream", h.ServerLogsStream)

	pprofGroup := group.Group("/debug/pprof", authMiddleware.WithAdminRequired().Add())
	pprofGroup.GET("", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	pprofGroup.GET("/", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	pprofGroup.GET("/cmdline", echo.WrapHandler(http.HandlerFunc(pprof.Cmdline)))
	pprofGroup.GET("/profile", echo.WrapHandler(http.HandlerFunc(pprof.Profile)))
	pprofGroup.POST("/symbol", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	pprofGroup.GET("/symbol", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	pprofGroup.GET("/trace", echo.WrapHandler(http.HandlerFunc(pprof.Trace)))
	// pprof.Index only serves a named profile when the request path starts with the
	// literal "/debug/pprof/". This group is mounted under /api, so that never
	// matches and every named profile would render the index page instead.
	pprofGroup.GET("/:name", func(c *echo.Context) error {
		pprof.Handler(c.Param("name")).ServeHTTP(c.Response(), c.Request())
		return nil
	})
}

// DiagnosticsStream pushes a snapshot on connect and every diagnosticsStreamInterval,
// and answers refresh, dump, leak-scan, and profile commands from the client.
func (h *WebSocketHandler) DiagnosticsStream(c *echo.Context) error {
	ps, _ := c.Get(string(middleware.ContextKeyUserPermissions)).(*authz.PermissionSet)
	snapshot := func(context.Context) any {
		d := buildDiagnosticsInternal(h.diagnosticsService)
		return systemtypes.DiagnosticsMessage{Type: "snapshot", Snapshot: &d}
	}
	return h.streamSnapshotsInternal(c, diagnosticsStreamInterval, snapshot, func(ctx context.Context, cmd systemtypes.DiagnosticsCommand) any {
		if cmd.Type == "refresh" {
			return snapshot(ctx)
		}
		result := systemtypes.DiagnosticsMessage{Type: "result", ID: cmd.ID}
		var err error
		switch cmd.Type {
		case "leakScan":
			report, scanErr := h.diagnosticsService.ScanGoroutineLeaks()
			result.LeakReport, err = &report, scanErr
		case "dump", "profile":
			if !ps.IsGlobalAdmin() {
				result.Error = "profiles require a global administrator"
				return result
			}
			if cmd.Type == "dump" {
				var text []byte
				text, err = h.diagnosticsService.Profile(ctx, cmd.Name, 0, 2)
				result.Text = string(text)
			} else {
				result.Data, err = h.diagnosticsService.Profile(ctx, cmd.Name, min(cmp.Or(cmd.Seconds, 30), 120), 0)
			}
		default:
			err = fmt.Errorf("unknown diagnostics command %q", cmd.Type)
		}
		if err != nil {
			result.Error = err.Error()
		}
		return result
	})
}

// ActorDiagnosticsStream pushes actor diagnostics for every environment on
// connect and then every actorDiagnosticsStreamInterval.
func (h *WebSocketHandler) ActorDiagnosticsStream(c *echo.Context) error {
	return h.streamSnapshotsInternal(c, actorDiagnosticsStreamInterval, func(ctx context.Context) any {
		return h.diagnosticsService.CollectAllActors(ctx)
	}, nil)
}

// streamSnapshotsInternal writes build's result on connect and every interval until
// the client disconnects. A non-nil handle answers client commands concurrently.
func (h *WebSocketHandler) streamSnapshotsInternal(c *echo.Context, interval time.Duration, build func(context.Context) any, handle func(context.Context, systemtypes.DiagnosticsCommand) any) error {
	conn, unregister, accepted := h.acceptWS(c, systemtypes.WSKindDiagnostics, c.Request().URL.Path)
	if !accepted {
		return nil
	}
	defer unregister()
	defer func() {
		if closeNowErr := conn.CloseNow(); closeNowErr != nil {
			slog.DebugContext(c.Request().Context(), "Failed to close diagnostics websocket connection", "error", closeNowErr)
		}
	}()

	ctx := c.Request().Context()
	write := func(v any) bool {
		b, marshalErr := json.Marshal(v)
		if marshalErr != nil {
			return true
		}
		wctx, cancel := context.WithTimeout(ctx, diagnosticsWriteWait)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}
	var onMessage func([]byte)
	if handle != nil {
		onMessage = func(b []byte) {
			var cmd systemtypes.DiagnosticsCommand
			if json.Unmarshal(b, &cmd) != nil {
				return
			}
			go write(handle(ctx, cmd))
		}
	}
	done := diagnosticsReadLoopInternal(ctx, conn, onMessage)

	if !write(build(ctx)) {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pingTicker := time.NewTicker(diagnosticsPingPeriod)
	defer pingTicker.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			if !write(build(ctx)) {
				return nil
			}
		case <-pingTicker.C:
			if !pingDiagnosticsConnInternal(ctx, conn) {
				return nil
			}
		}
	}
}

// ServerLogsStream replays the recent backend log backlog then streams new entries live.
func (h *WebSocketHandler) ServerLogsStream(c *echo.Context) error {
	conn, unregister, accepted := h.acceptWS(c, systemtypes.WSKindDiagnostics, c.Request().URL.Path)
	if !accepted {
		return nil
	}
	defer unregister()
	defer func() {
		if closeNowErr := conn.CloseNow(); closeNowErr != nil {
			slog.DebugContext(c.Request().Context(), "Failed to close server logs websocket connection", "error", closeNowErr)
		}
	}()

	// Subscribe before replaying the backlog so no entry is missed in the gap; at
	// worst the newest backlog entry is delivered twice, which is harmless.
	ch, cancel := defaultLogBroadcaster.Subscribe()
	defer cancel()

	ctx := c.Request().Context()
	done := diagnosticsReadLoopInternal(ctx, conn, nil)
	write := func(e logs.Entry) bool {
		b, marshalErr := json.Marshal(e)
		if marshalErr != nil {
			return true
		}
		wctx, wcancel := context.WithTimeout(ctx, diagnosticsWriteWait)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}

	for _, e := range defaultLogBroadcaster.Recent() {
		if !write(e) {
			return nil
		}
	}
	pingTicker := time.NewTicker(diagnosticsPingPeriod)
	defer pingTicker.Stop()
	for {
		select {
		case <-done:
			return nil
		case e, ok := <-ch:
			if !ok {
				return nil
			}
			if !write(e) {
				return nil
			}
		case <-pingTicker.C:
			if !pingDiagnosticsConnInternal(ctx, conn) {
				return nil
			}
		}
	}
}

// pingDiagnosticsConnInternal reports whether the peer answered a ping in time.
// The pong is serviced by the diagnosticsReadLoopInternal reader, so a peer
// that stops answering fails here within diagnosticsPingWait.
func pingDiagnosticsConnInternal(ctx context.Context, conn *websocket.Conn) bool {
	pctx, cancel := context.WithTimeout(ctx, diagnosticsPingWait)
	defer cancel()
	return conn.Ping(pctx) == nil
}

// diagnosticsReadLoopInternal passes incoming frames to onMessage, or drains them when
// it is nil; the returned channel closes when the peer disconnects.
func diagnosticsReadLoopInternal(ctx context.Context, conn *websocket.Conn, onMessage func([]byte)) <-chan struct{} {
	conn.SetReadLimit(diagnosticsReadLimit)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, b, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if onMessage != nil {
				onMessage(b)
			}
		}
	}()
	return done
}

var defaultLogBroadcaster = logs.New(1000)

// LogBroadcaster returns the backend-wide log broadcaster used by diagnostics.
func LogBroadcaster() *logs.Broadcaster {
	return defaultLogBroadcaster
}
