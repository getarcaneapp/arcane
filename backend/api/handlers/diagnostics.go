package handlers

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/getarcaneapp/arcane/types/v3"

	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/handlerutil"
)

// DiagnosticsHandler serves the environment-scoped actor and goroutine diagnostics.
// The live diagnostics snapshots use the api/ws streams.
type DiagnosticsHandler struct {
	diag *diagnostics.DiagnosticsService
}

type GetEnvironmentActorDiagnosticsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentActorDiagnosticsOutput struct {
	Body types.ActorDiagnostics
}

type GetEnvironmentGoroutineDiagnosticsInput struct {
	EnvironmentID string `path:"id" doc:"Environment ID"`
}

type GetEnvironmentGoroutineDiagnosticsOutput struct {
	Body types.GoroutineProfile
}

// RegisterDiagnostics registers the Huma diagnostics REST endpoints.
func RegisterDiagnostics(api huma.API, diag *diagnostics.DiagnosticsService) {
	h := &DiagnosticsHandler{diag: diag}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-environment-actor-diagnostics",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/diagnostics/actors",
		Summary:     "Get actor diagnostics for an environment",
		Description: "Returns Francis actor host, actor type, and run queue diagnostics for one environment.",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsRead, h.GetEnvironmentActorDiagnostics)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-environment-goroutine-diagnostics",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/diagnostics/goroutines",
		Summary:     "Get goroutine diagnostics for an environment",
		Description: "Returns the live goroutines grouped by identical stack, largest group first.",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsRead, h.GetEnvironmentGoroutineDiagnostics)
}

func (h *DiagnosticsHandler) GetEnvironmentActorDiagnostics(ctx context.Context, _ *GetEnvironmentActorDiagnosticsInput) (*GetEnvironmentActorDiagnosticsOutput, error) {
	d, err := h.diag.CollectActors(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to collect actor diagnostics: " + err.Error())
	}
	return &GetEnvironmentActorDiagnosticsOutput{Body: d}, nil
}

func (h *DiagnosticsHandler) GetEnvironmentGoroutineDiagnostics(_ context.Context, _ *GetEnvironmentGoroutineDiagnosticsInput) (*GetEnvironmentGoroutineDiagnosticsOutput, error) {
	profile, err := h.diag.Goroutines()
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to collect goroutine diagnostics: " + err.Error())
	}
	return &GetEnvironmentGoroutineDiagnosticsOutput{Body: profile}, nil
}
