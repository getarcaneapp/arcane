package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/getarcaneapp/arcane/backend/api/ws"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	wshub "github.com/getarcaneapp/arcane/backend/pkg/libarcane/ws"
	"github.com/labstack/echo/v4"
)

type DiagnosticsHandler struct {
	wsMetrics *ws.WebSocketMetrics
}

func RegisterDiagnosticsRoutes(group *echo.Group, authMiddleware *middleware.AuthMiddleware, wsMetrics *ws.WebSocketMetrics) {
	h := &DiagnosticsHandler{wsMetrics: wsMetrics}

	diagnostics := group.Group("/diagnostics", authMiddleware.Add())
	diagnostics.GET("/ws", h.WebSocketDiagnostics)
}

func (h *DiagnosticsHandler) WebSocketDiagnostics(c echo.Context) error {
	ps, _ := c.Get("userPermissions").(*authz.PermissionSet)
	if !ps.IsGlobalAdmin() {
		return c.JSON(http.StatusForbidden, map[string]any{"error": "Admin access required"})
	}

	metrics := h.wsMetrics.Snapshot()
	connections := h.wsMetrics.Connections()

	return c.JSON(http.StatusOK, map[string]any{
		"timestamp":         time.Now().UTC().Format(time.RFC3339Nano),
		"goroutines":        runtime.NumGoroutine(),
		"wsWorkerGoroutine": wshub.CountWorkerGoroutines(),
		"gomaxprocs":        runtime.GOMAXPROCS(0),
		"goVersion":         runtime.Version(),
		"activeConnections": metrics,
		"connections":       connections,
	})
}
