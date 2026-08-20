package handlers

import (
	"context"
	"net/http"

	"emperror.dev/errors"
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

type ScanGoroutineLeaksInput struct{}

type ScanGoroutineLeaksOutput struct {
	Body system.GoroutineLeakReport
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

	huma.Register(api, huma.Operation{
		OperationID: "scan-goroutine-leaks",
		Method:      http.MethodPost,
		Path:        "/diagnostics/goroutineleak",
		Summary:     "Scan for leaked goroutines",
		Description: "Runs a goroutine leak-detection GC cycle and returns the goroutineleak pprof profile as text (leaked stacks only).",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
		Middlewares: middleware.RequirePermission(api, authz.PermDiagnosticsRead),
	}, h.ScanGoroutineLeaks)
}

func (h *DiagnosticsHandler) GetDiagnostics(_ context.Context, _ *DiagnosticsInput) (*GetDiagnosticsOutput, error) {
	return &GetDiagnosticsOutput{Body: ws.BuildDiagnostics(h.diag)}, nil
}

func (h *DiagnosticsHandler) GetRecentLogs(_ context.Context, _ *DiagnosticsInput) (*GetDiagnosticsLogsOutput, error) {
	return &GetDiagnosticsLogsOutput{Body: ws.LogBroadcaster().Recent()}, nil
}

func (h *DiagnosticsHandler) ScanGoroutineLeaks(_ context.Context, _ *ScanGoroutineLeaksInput) (*ScanGoroutineLeaksOutput, error) {
	report, err := h.diag.ScanGoroutineLeaks()
	if err != nil {
		return nil, huma.Error500InternalServerError(errors.WithMessage(err, "Failed to collect goroutine leak profile").Error())
	}
	return &ScanGoroutineLeaksOutput{Body: report}, nil
}
