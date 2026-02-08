package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	ws "github.com/getarcaneapp/arcane/backend/internal/utils/ws"
	"github.com/gin-gonic/gin"
)

type DiagnosticsHandler struct {
	wsMetrics *WebSocketMetrics
}

func RegisterDiagnosticsRoutes(group *gin.RouterGroup, authMiddleware *middleware.AuthMiddleware, wsMetrics *WebSocketMetrics) {
	h := &DiagnosticsHandler{wsMetrics: wsMetrics}

	diagnostics := group.Group("/diagnostics")
	diagnostics.Use(authMiddleware.Add())
	{
		diagnostics.GET("/ws", h.WebSocketDiagnostics)
	}
}

func (h *DiagnosticsHandler) WebSocketDiagnostics(c *gin.Context) {
	isAdmin, _ := c.Get("userIsAdmin")
	if admin, ok := isAdmin.(bool); !ok || !admin {
		c.JSON(http.StatusForbidden, gin.H{"error": "Admin access required"})
		return
	}

	metrics := h.wsMetrics.Snapshot()
	connections := h.wsMetrics.Connections()

	c.JSON(http.StatusOK, gin.H{
		"timestamp":         time.Now().UTC().Format(time.RFC3339Nano),
		"goroutines":        runtime.NumGoroutine(),
		"wsWorkerGoroutine": ws.CountWorkerGoroutines(),
		"gomaxprocs":        runtime.GOMAXPROCS(0),
		"goVersion":         runtime.Version(),
		"activeConnections": metrics,
		"connections":       connections,
	})
}
