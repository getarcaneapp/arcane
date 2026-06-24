package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	humamw "github.com/getarcaneapp/arcane/backend/v2/api/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/api/ws"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/logstream"
	"github.com/getarcaneapp/arcane/types/v2/system"
)

// diagnosticsHandler serves the REST diagnostics endpoints. The live WebSocket
// streams and pprof routes live in the api/ws package alongside the other
// streaming endpoints; the snapshot is assembled there too (ws.BuildDiagnostics).
type diagnosticsHandler struct {
	diag *services.DiagnosticsService
}

type diagnosticsInput struct{}

type getDiagnosticsOutput struct {
	Body system.Diagnostics
}

type getDiagnosticsLogsOutput struct {
	Body []system.LogEntry
}

// RegisterDiagnostics registers the Huma diagnostics REST endpoints.
func RegisterDiagnostics(api huma.API, diag *services.DiagnosticsService) {
	h := &diagnosticsHandler{diag: diag}

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
	}, h.getDiagnosticsInternal)

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
	}, h.getRecentLogsInternal)
}

func (h *diagnosticsHandler) getDiagnosticsInternal(_ context.Context, _ *diagnosticsInput) (*getDiagnosticsOutput, error) {
	return &getDiagnosticsOutput{Body: ws.BuildDiagnostics(h.diag)}, nil
}

func (h *diagnosticsHandler) getRecentLogsInternal(_ context.Context, _ *diagnosticsInput) (*getDiagnosticsLogsOutput, error) {
	return &getDiagnosticsLogsOutput{Body: logstream.Default().Recent()}, nil
}
