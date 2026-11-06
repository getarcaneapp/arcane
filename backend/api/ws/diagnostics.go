package ws

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/labstack/echo/v5"

	"kit.getarcane.app/docker/streams/logs"

	"getarcane.dev/arcane/v3/internal/diagnostics"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	wshub "getarcane.dev/arcane/v3/pkg/ws"
	"getarcane.dev/types/v3"
)

const (
	diagnosticsStreamInterval = 2 * time.Second
	// actorDiagnosticsStreamInterval is slower because each push queries every environment.
	actorDiagnosticsStreamInterval = 10 * time.Second
	diagnosticsReadLimit           = 512
	diagnosticsWriteWait           = 10 * time.Second
	diagnosticsPingWait            = 10 * time.Second
	diagnosticsPingPeriod          = 54 * time.Second
)

var (
	defaultLogBroadcaster = logs.New(logs.Config{Capacity: 1000})

	diagnosticsCommandConditions = authz.PermissionConditions{
		{
			In:         authz.ConditionInMessage,
			Field:      "type",
			Value:      "dump",
			Permission: authz.PermDiagnosticsProfile,
		},
		{
			In:         authz.ConditionInMessage,
			Field:      "type",
			Value:      "profile",
			Permission: authz.PermDiagnosticsProfile,
		},
	}
)

func buildDiagnostics(diag *diagnostics.DiagnosticsService) types.Diagnostics {
	d := types.Diagnostics{Timestamp: time.Now().UTC()}
	if diag != nil {
		d.Runtime, d.Memory, d.GC = diag.Collect()
	}
	d.Runtime.WSWorkerGoroutines = int(wshub.WorkerGoroutines.Load())
	d.WebSocket = types.WebSocketDiagnostics{
		Snapshot:    defaultWebSocketMetrics.Snapshot(),
		Connections: defaultWebSocketMetrics.Connections(),
	}
	return d
}

func (h *WebSocketHandler) diagnosticsRoutes() []middleware.EchoRoute {
	return []middleware.EchoRoute{
		{
			Method:     http.MethodGet,
			Path:       "/diagnostics/stream",
			Permission: authz.PermDiagnosticsRead,
			Conditions: diagnosticsCommandConditions,
			Handler:    h.DiagnosticsStream,
		},
		{
			Method:     http.MethodGet,
			Path:       "/diagnostics/actors/stream",
			Permission: authz.PermDiagnosticsRead,
			Handler:    h.ActorDiagnosticsStream,
		},
		{
			Method:     http.MethodGet,
			Path:       "/diagnostics/logs/stream",
			Permission: authz.PermDiagnosticsRead,
			Handler:    h.ServerLogsStream,
		},
	}
}

func (h *WebSocketHandler) DiagnosticsStream(c *echo.Context) error {
	ps, _ := c.Get(string(middleware.ContextKeyUserPermissions)).(*authz.PermissionSet)
	snapshot := func(context.Context) any {
		d := buildDiagnostics(h.diagnosticsService)
		return types.DiagnosticsMessage{Type: "snapshot", Snapshot: &d}
	}
	return h.streamSnapshots(c, diagnosticsStreamInterval, snapshot, func(ctx context.Context, cmd types.DiagnosticsCommand) any {
		if cmd.Type == "refresh" {
			return snapshot(ctx)
		}
		result := types.DiagnosticsMessage{Type: "result", ID: cmd.ID}
		for _, perm := range diagnosticsCommandConditions.Required("type", cmd.Type) {
			if !ps.Allows(perm, "") {
				result.Error = "permission denied: " + perm
				return result
			}
		}
		var err error
		switch cmd.Type {
		case "leakScan":
			report, scanErr := h.diagnosticsService.ScanGoroutineLeaks()
			result.LeakReport, err = &report, scanErr
		case "dump":
			var text []byte
			text, err = h.diagnosticsService.Profile(ctx, cmd.Name, 0, 2)
			result.Text = string(text)
		case "profile":
			result.Data, err = h.diagnosticsService.Profile(ctx, cmd.Name, min(cmp.Or(cmd.Seconds, 30), 120), 0)
		default:
			err = fmt.Errorf("unknown diagnostics command %q", cmd.Type)
		}
		if err != nil {
			result.Error = err.Error()
		}
		return result
	})
}

func (h *WebSocketHandler) ActorDiagnosticsStream(c *echo.Context) error {
	return h.streamSnapshots(c, actorDiagnosticsStreamInterval, func(ctx context.Context) any {
		return h.diagnosticsService.CollectAllActors(ctx)
	}, nil)
}

func (h *WebSocketHandler) streamSnapshots(c *echo.Context, interval time.Duration, build func(context.Context) any, handle func(context.Context, types.DiagnosticsCommand) any) error {
	conn, unregister, accepted := h.acceptWS(c, types.WSKindDiagnostics, c.Request().URL.Path)
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
		commands := make(chan types.DiagnosticsCommand, 1)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case cmd := <-commands:
					write(handle(ctx, cmd))
				}
			}
		}()
		onMessage = func(b []byte) {
			var cmd types.DiagnosticsCommand
			if json.Unmarshal(b, &cmd) != nil {
				return
			}
			select {
			case commands <- cmd:
			default:
				write(types.DiagnosticsMessage{
					Type:  "result",
					ID:    cmd.ID,
					Error: "busy",
				})
			}
		}
	}
	done := diagnosticsReadLoop(ctx, conn, onMessage)

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
			if !pingDiagnosticsConn(ctx, conn) {
				return nil
			}
		}
	}
}

func (h *WebSocketHandler) ServerLogsStream(c *echo.Context) error {
	conn, unregister, accepted := h.acceptWS(c, types.WSKindDiagnostics, c.Request().URL.Path)
	if !accepted {
		return nil
	}
	defer unregister()
	defer func() {
		if closeNowErr := conn.CloseNow(); closeNowErr != nil {
			slog.DebugContext(c.Request().Context(), "Failed to close server logs websocket connection", "error", closeNowErr)
		}
	}()

	ch, cancel := defaultLogBroadcaster.Subscribe()
	defer cancel()

	ctx := c.Request().Context()
	done := diagnosticsReadLoop(ctx, conn, nil)
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
			if !pingDiagnosticsConn(ctx, conn) {
				return nil
			}
		}
	}
}

func pingDiagnosticsConn(ctx context.Context, conn *websocket.Conn) bool {
	pctx, cancel := context.WithTimeout(ctx, diagnosticsPingWait)
	defer cancel()
	return conn.Ping(pctx) == nil
}

func diagnosticsReadLoop(ctx context.Context, conn *websocket.Conn, onMessage func([]byte)) <-chan struct{} {
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

func LogBroadcaster() *logs.Broadcaster {
	return defaultLogBroadcaster
}
