package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/getarcaneapp/arcane/backend/v2/api/ws"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/types/v2/system"
	"go.getarcane.app/streams/logs"
)

// DiagnosticsHandler serves the REST diagnostics endpoints. The live WebSocket
// streams and pprof routes live in the api/ws package alongside the other
// streaming endpoints; the snapshot is assembled there too (ws.BuildDiagnostics).
type DiagnosticsHandler struct {
	diag *diagnostics.DiagnosticsService
}

type DiagnosticsInput struct{}

type GetDiagnosticsOutput struct {
	Body system.Diagnostics
}

type GetDiagnosticsLogsOutput struct {
	Body []logs.Entry
}

// RegisterDiagnostics registers the Huma diagnostics REST endpoints.
func RegisterDiagnostics(api huma.API, diag *diagnostics.DiagnosticsService) {
	h := &DiagnosticsHandler{diag: diag}

	huma.Register(api, huma.Operation{
		OperationID: "get-diagnostics",
		Method:      http.MethodGet,
		Path:        "/diagnostics",
		Summary:     "Get runtime diagnostics",
		Description: "Returns Go runtime, memory, garbage-collector, and WebSocket connection statistics.",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermDiagnosticsRead),
	}, h.GetDiagnostics)

	huma.Register(api, huma.Operation{
		OperationID: "get-diagnostics-logs",
		Method:      http.MethodGet,
		Path:        "/diagnostics/logs",
		Summary:     "Get recent backend logs",
		Description: "Returns the most recent buffered backend log entries (oldest first).",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermDiagnosticsRead),
	}, h.GetRecentLogs)
}

func (h *DiagnosticsHandler) GetDiagnostics(_ context.Context, _ *DiagnosticsInput) (*GetDiagnosticsOutput, error) {
	return &GetDiagnosticsOutput{Body: ws.BuildDiagnostics(h.diag)}, nil
}

func (h *DiagnosticsHandler) GetRecentLogs(_ context.Context, _ *DiagnosticsInput) (*GetDiagnosticsLogsOutput, error) {
	return &GetDiagnosticsLogsOutput{Body: ws.LogBroadcaster().Recent()}, nil
}
