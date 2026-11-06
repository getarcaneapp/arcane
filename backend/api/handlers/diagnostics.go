package handlers

import (
	"context"
	"net/http"
	"net/http/pprof"

	"github.com/danielgtaylor/huma/v2"
	"github.com/labstack/echo/v5"

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

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-index",
		Method:      http.MethodGet,
		Path:        "/debug/pprof",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Index))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-index-slash",
		Method:      http.MethodGet,
		Path:        "/debug/pprof/",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Index))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-cmdline",
		Method:      http.MethodGet,
		Path:        "/debug/pprof/cmdline",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Cmdline))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-profile",
		Method:      http.MethodGet,
		Path:        "/debug/pprof/profile",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Profile))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-symbol-lookup",
		Method:      http.MethodPost,
		Path:        "/debug/pprof/symbol",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Symbol))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-symbol",
		Method:      http.MethodGet,
		Path:        "/debug/pprof/symbol",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Symbol))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-trace",
		Method:      http.MethodGet,
		Path:        "/debug/pprof/trace",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(echo.WrapHandler(http.HandlerFunc(pprof.Trace))))

	middleware.RegisterWithPermission(api, huma.Operation{
		OperationID: "pprof-named-profile",
		Method:      http.MethodGet,
		Path:        "/debug/pprof/{name}",
		Tags:        []string{"Diagnostics"},
		Hidden:      true,
		Security:    handlerutil.DefaultOperationSecurity(),
	}, authz.PermDiagnosticsProfile, middleware.EchoHandler(func(c *echo.Context) error {
		pprof.Handler(c.Param("name")).ServeHTTP(c.Response(), c.Request())
		return nil
	}))
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
