package handlers

import (
	"cmp"
	"context"
	"math"
	"net/http"
	"net/http/pprof"
	"strconv"

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

const maxProfileSeconds = 120

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
	secondsParam := &huma.Param{
		Name:        "seconds",
		In:          "query",
		Description: "Capture duration in whole seconds, capped at 120.",
		Schema:      &huma.Schema{Type: huma.TypeInteger},
	}
	traceSecondsParam := &huma.Param{
		Name:        "seconds",
		In:          "query",
		Description: "Capture duration in seconds, capped at 120.",
		Schema:      &huma.Schema{Type: huma.TypeNumber},
	}
	debugParam := &huma.Param{
		Name:        "debug",
		In:          "query",
		Description: "Set to 1 or 2 for a text dump instead of the binary profile.",
		Schema:      &huma.Schema{Type: huma.TypeInteger},
	}
	binary := map[string]*huma.MediaType{
		"application/octet-stream": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
	}
	text := map[string]*huma.MediaType{
		"text/plain": {Schema: &huma.Schema{Type: huma.TypeString}},
	}
	html := map[string]*huma.MediaType{
		"text/html": {Schema: &huma.Schema{Type: huma.TypeString}},
	}
	pprofRoutes := []struct {
		handler echo.HandlerFunc
		content map[string]*huma.MediaType
		headers map[string]*huma.Param
		id      string
		method  string
		path    string
		summary string
		params  []*huma.Param
		status  int
	}{
		{
			id:      "pprof-index",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof",
			summary: "Redirect to the pprof index",
			params:  []*huma.Param{idParam},
			status:  http.StatusMovedPermanently,
			headers: map[string]*huma.Param{
				"Location": {Description: "The pprof index URL with a trailing slash.", Schema: &huma.Schema{Type: huma.TypeString}},
			},
			handler: func(c *echo.Context) error {
				return c.Redirect(http.StatusMovedPermanently, "pprof/")
			},
		},
		{
			id:      "pprof-index-slash",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof/",
			summary: "List available pprof profiles",
			params:  []*huma.Param{idParam},
			content: html,
			handler: echo.WrapHandler(http.HandlerFunc(pprof.Index)),
		},
		{
			id:      "pprof-cmdline",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof/cmdline",
			summary: "Get the process command line",
			params:  []*huma.Param{idParam},
			content: text,
			handler: echo.WrapHandler(http.HandlerFunc(pprof.Cmdline)),
		},
		{
			id:      "pprof-profile",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof/profile",
			summary: "Capture a CPU profile",
			params:  []*huma.Param{idParam, secondsParam},
			content: binary,
			handler: capProfileSeconds(pprof.Profile, cpuSecondsOverLimit),
		},
		{
			id:      "pprof-symbol-lookup",
			method:  http.MethodPost,
			path:    "/agents/{id}/debug/pprof/symbol",
			summary: "Look up program counters",
			params:  []*huma.Param{idParam},
			content: text,
			handler: echo.WrapHandler(http.HandlerFunc(pprof.Symbol)),
		},
		{
			id:      "pprof-symbol",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof/symbol",
			summary: "Look up program counters",
			params:  []*huma.Param{idParam},
			content: text,
			handler: echo.WrapHandler(http.HandlerFunc(pprof.Symbol)),
		},
		{
			id:      "pprof-trace",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof/trace",
			summary: "Capture an execution trace",
			params:  []*huma.Param{idParam, traceSecondsParam},
			content: binary,
			handler: capProfileSeconds(pprof.Trace, traceSecondsOverLimit),
		},
		{
			id:      "pprof-named-profile",
			method:  http.MethodGet,
			path:    "/agents/{id}/debug/pprof/{name}",
			summary: "Get a named runtime profile",
			params:  []*huma.Param{idParam, nameParam, debugParam, secondsParam},
			content: map[string]*huma.MediaType{
				"application/octet-stream": binary["application/octet-stream"],
				"text/plain":               text["text/plain"],
			},
			handler: func(c *echo.Context) error {
				return capProfileSeconds(pprof.Handler(c.Param("name")).ServeHTTP, cpuSecondsOverLimit)(c)
			},
		},
	}
	for _, r := range pprofRoutes {
		middleware.RegisterWithPermission(api, huma.Operation{
			OperationID:   r.id,
			Method:        r.method,
			Path:          r.path,
			Summary:       r.summary,
			DefaultStatus: cmp.Or(r.status, http.StatusOK),
			Parameters:    r.params,
			Responses: map[string]*huma.Response{
				strconv.Itoa(cmp.Or(r.status, http.StatusOK)): {
					Description: http.StatusText(cmp.Or(r.status, http.StatusOK)),
					Content:     r.content,
					Headers:     r.headers,
				},
			},
			Tags:     []string{"Diagnostics"},
			Security: handlerutil.DefaultOperationSecurity(),
		}, authz.PermDiagnosticsProfile, middleware.EchoHandler(r.handler))
	}
}

func capProfileSeconds(handler http.HandlerFunc, overLimit func(seconds string) bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		req := c.Request()
		query := req.URL.Query()
		if overLimit(query.Get("seconds")) {
			query.Set("seconds", strconv.Itoa(maxProfileSeconds))
			req = req.Clone(req.Context())
			req.URL.RawQuery = query.Encode()
			req.Form = nil
		}
		handler(c.Response(), req)
		return nil
	}
}

func cpuSecondsOverLimit(seconds string) bool {
	parsed, err := strconv.ParseInt(seconds, 10, 64)
	return err == nil && parsed > maxProfileSeconds
}

func traceSecondsOverLimit(seconds string) bool {
	parsed, err := strconv.ParseFloat(seconds, 64)
	return err == nil && (parsed > maxProfileSeconds || math.IsNaN(parsed))
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
