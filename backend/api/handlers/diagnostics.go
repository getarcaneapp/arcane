package handlers

import (
	"context"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"

	"kit.getarcane.app"

	"getarcane.dev/arcane/v3/internal/diagnostics"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/types/v3"
)

type (
	DiagnosticsHandler struct {
		diag *diagnostics.DiagnosticsService
	}

	GetEnvironmentActorDiagnosticsOutput struct {
		Body types.ActorDiagnostics
	}

	GetEnvironmentGoroutineDiagnosticsOutput struct {
		Body types.GoroutineProfile
	}
)

func RegisterDiagnostics(api huma.API, diag *diagnostics.DiagnosticsService) {
	h := &DiagnosticsHandler{diag: diag}

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-environment-actor-diagnostics",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/diagnostics/actors",
		Summary:     "Get actor diagnostics for an environment",
		Description: "Returns Francis actor host, actor type, and run queue diagnostics for one environment.",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsRead, h.GetEnvironmentActorDiagnostics)

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "get-environment-goroutine-diagnostics",
		Method:      http.MethodGet,
		Path:        "/agents/{id}/diagnostics/goroutines",
		Summary:     "Get goroutine diagnostics for an environment",
		Description: "Returns the live goroutines grouped by identical stack, largest group first.",
		Tags:        []string{"Diagnostics"},
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsRead, h.GetEnvironmentGoroutineDiagnostics)

	pprofRoutes := []struct {
		handler echo.HandlerFunc
		id      string
		method  string
		path    string
	}{
		{id: "pprof-index", method: http.MethodGet, path: "/agents/{id}/debug/pprof", handler: echo.WrapHandler(http.HandlerFunc(pprof.Index))},
		{id: "pprof-index-slash", method: http.MethodGet, path: "/agents/{id}/debug/pprof/", handler: echo.WrapHandler(http.HandlerFunc(pprof.Index))},
		{id: "pprof-cmdline", method: http.MethodGet, path: "/agents/{id}/debug/pprof/cmdline", handler: echo.WrapHandler(http.HandlerFunc(pprof.Cmdline))},
		{id: "pprof-profile", method: http.MethodGet, path: "/agents/{id}/debug/pprof/profile", handler: echo.WrapHandler(http.HandlerFunc(pprof.Profile))},
		{id: "pprof-symbol-lookup", method: http.MethodPost, path: "/agents/{id}/debug/pprof/symbol", handler: echo.WrapHandler(http.HandlerFunc(pprof.Symbol))},
		{id: "pprof-symbol", method: http.MethodGet, path: "/agents/{id}/debug/pprof/symbol", handler: echo.WrapHandler(http.HandlerFunc(pprof.Symbol))},
		{id: "pprof-trace", method: http.MethodGet, path: "/agents/{id}/debug/pprof/trace", handler: echo.WrapHandler(http.HandlerFunc(pprof.Trace))},
		{id: "pprof-named-profile", method: http.MethodGet, path: "/agents/{id}/debug/pprof/{name}", handler: func(c *echo.Context) error {
			pprof.Handler(c.Param("name")).ServeHTTP(c.Response(), c.Request())
			return nil
		}},
	}
	idParam := &huma.Param{
		Name:     "id",
		In:       "path",
		Required: true,
		Schema:   &huma.Schema{Type: huma.TypeString},
	}
	nameParam := &huma.Param{
		Name:     "name",
		In:       "path",
		Required: true,
		Schema:   &huma.Schema{Type: huma.TypeString},
	}
	for _, r := range pprofRoutes {
		middleware.RegisterWithPermission(api, huma.Operation{
			OperationID: r.id,
			Method:      r.method,
			Path:        r.path,
			Parameters:  kit.Ternary(strings.HasSuffix(r.path, "{name}"), []*huma.Param{idParam, nameParam}, []*huma.Param{idParam}),
			Tags:        []string{"Diagnostics"},
			Security:    handlerutil.DefaultOperationSecurity(),
		}, authz.PermDiagnosticsProfile, middleware.EchoHandler(r.handler))
	}
}

func (h *DiagnosticsHandler) GetEnvironmentActorDiagnostics(ctx context.Context, _ *types.EnvironmentInput) (*GetEnvironmentActorDiagnosticsOutput, error) {
	d, err := h.diag.CollectActors(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to collect actor diagnostics: " + err.Error())
	}
	return &GetEnvironmentActorDiagnosticsOutput{Body: d}, nil
}

func (h *DiagnosticsHandler) GetEnvironmentGoroutineDiagnostics(_ context.Context, _ *types.EnvironmentInput) (*GetEnvironmentGoroutineDiagnosticsOutput, error) {
	profile, err := h.diag.Goroutines()
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to collect goroutine diagnostics: " + err.Error())
	}
	return &GetEnvironmentGoroutineDiagnosticsOutput{Body: profile}, nil
}
