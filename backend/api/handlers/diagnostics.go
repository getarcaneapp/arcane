package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	humamw "github.com/getarcaneapp/arcane/backend/api/middleware"
	"github.com/getarcaneapp/arcane/backend/api/ws"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/logstream"
	"github.com/getarcaneapp/arcane/types/system"
)

// DiagnosticsHandler serves the REST diagnostics endpoints. The live WebSocket
// streams and pprof routes live in the api/ws package alongside the other
// streaming endpoints; the snapshot is assembled there too (ws.BuildDiagnostics).
type DiagnosticsHandler struct {
	diag *services.DiagnosticsService
}

type DiagnosticsInput struct{}

type GetDiagnosticsOutput struct {
	Body system.Diagnostics
}

type GetDiagnosticsLogsOutput struct {
	Body []system.LogEntry
}

// RegisterDiagnostics registers the Huma diagnostics REST endpoints.
func RegisterDiagnostics(api huma.API, diag *services.DiagnosticsService) {
	h := &DiagnosticsHandler{diag: diag}

	huma.Register(api, huma.Operation{
		OperationID: "get-diagnostics",
		Method:      http.MethodGet,
		Path:        "/diagnostics",
		Summary:     "Get runtime diagnostics",
		Description: "Returns Go runtime, memory, garbage-collector, and WebSocket connection statistics.",
		Tags:        []string{"Diagnostics"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermDiagnosticsRead),
	}, h.GetDiagnostics)

	huma.Register(api, huma.Operation{
		OperationID: "get-diagnostics-logs",
		Method:      http.MethodGet,
		Path:        "/diagnostics/logs",
		Summary:     "Get recent backend logs",
		Description: "Returns the most recent buffered backend log entries (oldest first).",
		Tags:        []string{"Diagnostics"},
		Security: []map[string][]string{
			{"BearerAuth": {}},
			{"ApiKeyAuth": {}},
		},
		Middlewares: humamw.RequirePermission(api, authz.PermDiagnosticsRead),
	}, h.GetRecentLogs)
}

func (h *DiagnosticsHandler) GetDiagnostics(_ context.Context, _ *DiagnosticsInput) (*GetDiagnosticsOutput, error) {
	return &GetDiagnosticsOutput{Body: ws.BuildDiagnostics(h.diag)}, nil
}

func (h *DiagnosticsHandler) GetRecentLogs(_ context.Context, _ *DiagnosticsInput) (*GetDiagnosticsLogsOutput, error) {
	return &GetDiagnosticsLogsOutput{Body: logstream.Default().Recent()}, nil
}
