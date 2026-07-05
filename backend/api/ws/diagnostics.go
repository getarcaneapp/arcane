package ws

import (
	"encoding/json"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	wshub "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/ws"
	systemtypes "github.com/getarcaneapp/arcane/types/v2/system"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"go.getarcane.app/streams/logs"
)

// diagnosticsStreamInterval is how often the live diagnostics stream pushes a snapshot.
const diagnosticsStreamInterval = 2 * time.Second

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
func (h *WebSocketHandler) DiagnosticsStream(c echo.Context) error {
	conn, err := h.wsUpgrader.Upgrade(c.Response().Writer, c.Request(), nil)
	if err != nil {
		return nil
	}
	defer conn.Close()

	done := diagnosticsReadLoopInternal(conn)
	write := func() bool {
		b, marshalErr := json.Marshal(BuildDiagnostics(h.diagnosticsService))
		if marshalErr != nil {
			return true
		}
		return conn.WriteMessage(websocket.TextMessage, b) == nil
	}

	if !write() {
		return nil
	}
	ticker := time.NewTicker(diagnosticsStreamInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return nil
		case <-ticker.C:
			if !write() {
				return nil
			}
		}
	}
}

// ServerLogsStream replays the recent backend log backlog then streams new entries live.
func (h *WebSocketHandler) ServerLogsStream(c echo.Context) error {
	conn, err := h.wsUpgrader.Upgrade(c.Response().Writer, c.Request(), nil)
	if err != nil {
		return nil
	}
	defer conn.Close()

	// Subscribe before replaying the backlog so no entry is missed in the gap; at
	// worst the newest backlog entry is delivered twice, which is harmless.
	ch, cancel := defaultLogBroadcaster.Subscribe()
	defer cancel()

	done := diagnosticsReadLoopInternal(conn)
	write := func(e logs.Entry) bool {
		b, marshalErr := json.Marshal(e)
		if marshalErr != nil {
			return true
		}
		return conn.WriteMessage(websocket.TextMessage, b) == nil
	}

	for _, e := range defaultLogBroadcaster.Recent() {
		if !write(e) {
			return nil
		}
	}
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
		}
	}
}

// diagnosticsReadLoopInternal drains incoming frames; the returned channel closes
// when the peer disconnects, signaling the writer loop to stop.
func diagnosticsReadLoopInternal(conn *websocket.Conn) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	return done
}
