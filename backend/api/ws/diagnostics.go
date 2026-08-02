package ws

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/coder/websocket"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/streams/logs"
)

// diagnosticsStreamInterval is how often the live diagnostics stream pushes a snapshot.
const diagnosticsStreamInterval = 2 * time.Second

// Keepalive/liveness bounds for the diagnostics sockets, mirroring the system
// stats stream. Without them a silently dead peer wedges the writer forever,
// which also strands the writer's deferred cleanup (log subscription, conn).
const (
	diagnosticsReadLimit  = 512
	diagnosticsWriteWait  = 10 * time.Second
	diagnosticsPingWait   = 10 * time.Second
	diagnosticsPingPeriod = 54 * time.Second
)

// BuildDiagnostics assembles a full diagnostics snapshot: runtime/memory/GC from
// the DiagnosticsService plus this package's WebSocket metrics and worker-goroutine
// count. Shared by the REST endpoint (via handlers) and the live WebSocket stream.
func BuildDiagnostics(diag *services.DiagnosticsService) systemtypes.Diagnostics {
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
func (h *WebSocketHandler) registerDiagnosticsRoutesInternal(group *echo.Group, authMiddleware *middleware.AuthMiddleware) {
	diag := group.Group("/diagnostics",
		authMiddleware.WithAdminNotRequired().Add(),
		middleware.RequirePermission(authz.PermDiagnosticsRead),
	)
	diag.GET("/stream", h.DiagnosticsStream)
	diag.GET("/logs/stream", h.ServerLogsStream)

	pprofGroup := group.Group("/debug/pprof", authMiddleware.WithAdminRequired().Add())
	pprofGroup.GET("", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	pprofGroup.GET("/", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
	pprofGroup.GET("/cmdline", echo.WrapHandler(http.HandlerFunc(pprof.Cmdline)))
	pprofGroup.GET("/profile", echo.WrapHandler(http.HandlerFunc(pprof.Profile)))
	pprofGroup.POST("/symbol", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	pprofGroup.GET("/symbol", echo.WrapHandler(http.HandlerFunc(pprof.Symbol)))
	pprofGroup.GET("/trace", echo.WrapHandler(http.HandlerFunc(pprof.Trace)))
	pprofGroup.GET("/:name", echo.WrapHandler(http.HandlerFunc(pprof.Index)))
}

// DiagnosticsStream pushes a fresh diagnostics snapshot on connect and then every
// diagnosticsStreamInterval until the client disconnects.
func (h *WebSocketHandler) DiagnosticsStream(c *echo.Context) error {
	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		return nil
	}
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close diagnostics websocket connection", "error", err)
		}
	}()

	ctx := c.Request().Context()
	done := diagnosticsReadLoopInternal(ctx, conn)
	write := func() bool {
		b, marshalErr := json.Marshal(BuildDiagnostics(h.diagnosticsService))
		if marshalErr != nil {
			return true
		}
		wctx, cancel := context.WithTimeout(ctx, diagnosticsWriteWait)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}

	if !write() {
		return nil
	}
	ticker := time.NewTicker(diagnosticsStreamInterval)
	defer ticker.Stop()
	pingTicker := time.NewTicker(diagnosticsPingPeriod)
	defer pingTicker.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			if !write() {
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
	conn, err := wshub.Accept(c.Response(), c.Request(), h.checkWSOrigin)
	if err != nil {
		return nil
	}
	defer func() {
		if err := conn.CloseNow(); err != nil {
			slog.Debug("Failed to close server logs websocket connection", "error", err)
		}
	}()

	// Subscribe before replaying the backlog so no entry is missed in the gap; at
	// worst the newest backlog entry is delivered twice, which is harmless.
	ch, cancel := defaultLogBroadcaster.Subscribe()
	defer cancel()

	ctx := c.Request().Context()
	done := diagnosticsReadLoopInternal(ctx, conn)
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

// diagnosticsReadLoopInternal drains incoming frames; the returned channel closes
// when the peer disconnects, signaling the writer loop to stop.
func diagnosticsReadLoopInternal(ctx context.Context, conn *websocket.Conn) <-chan struct{} {
	conn.SetReadLimit(diagnosticsReadLimit)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	return done
}
