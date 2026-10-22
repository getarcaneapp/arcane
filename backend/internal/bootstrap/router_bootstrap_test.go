package bootstrap

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/api"
	"github.com/getarcaneapp/arcane/backend/v3/api/ws"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/health"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
)

func TestNewRouter_DecodesPathParams(t *testing.T) {
	cfg := &config.Config{
		AgentMode:   true,
		AppUrl:      "http://localhost:3552",
		Environment: config.AppEnvironmentTest,
	}
	router, _ := newRouter(RouterParams{
		Context: t.Context(),
		Config:  cfg,
		HandlerDeps: api.HandlerDeps{
			Project:   project.New(&project.ProjectService{}, nil),
			Container: container.New(&container.ContainerService{}, nil, nil, nil),
			Swarm:     swarm.New(&swarm.SwarmService{}, nil, nil, nil),
			System:    system.New(&system.SystemService{}, nil, nil),
			Health:    health.New(&health.Service{}),
			Settings:  newTestSettingsModule(t, cfg),
		},
		AuthMiddleware: auth.NewAuthMiddleware(nil, cfg),
		TunnelRegistry: edge.NewTunnelRegistry(),
	})
	humaAPI := humaecho.NewWithGroup(router, router.Group("/api"), huma.DefaultConfig("test", "1.0.0"))

	var got string
	huma.Register(humaAPI, huma.Operation{
		OperationID: "get-image",
		Method:      http.MethodGet,
		Path:        "/environments/{id}/images/{imageId}",
	}, func(_ context.Context, input *struct {
		ID      string `path:"id"`
		ImageID string `path:"imageId"`
	},
	) (*struct{}, error) {
		got = input.ImageID
		return nil, nil
	})

	// RFC 3986 §6.2.2.2: these are all the same URI and must resolve identically.
	for _, segment := range []string{"sha256:abc", "sha256%3Aabc", "%73ha256:abc"} {
		got = ""
		req := httptest.NewRequest(http.MethodGet, "/api/environments/0/images/"+segment, http.NoBody)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code, "segment %q", segment)
		require.Equal(t, "sha256:abc", got, "segment %q", segment)
	}
}

func TestSecureCookieContextMiddleware_TrustGating(t *testing.T) {
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")

	require.NoError(t, err,
		"parse cidr: %v", err)

	trusted := []*net.IPNet{loopback}

	runRequest := func(t *testing.T, nets []*net.IPNet, remoteAddr, forwardedProto string) bool {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest("GET", "/", http.NoBody)
		req.RemoteAddr = remoteAddr
		if forwardedProto != "" {
			req.Header.Set("X-Forwarded-Proto", forwardedProto)
		}
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		var observed bool
		handler := secureCookieContextMiddleware(nets)(func(c *echo.Context) error {
			observed = cookie.SecureCookieFromContext(c.Request().Context())
			return nil
		})
		{
			handlerErr := handler(c)
			require.NoError(t, handlerErr,
				"handler: %v", handlerErr)
		}

		return observed
	}

	t.Run("trusted proxy with X-Forwarded-Proto https sets secure", func(t *testing.T) {
		assert.True(t, runRequest(t, trusted, "127.0.0.1:54321", "https"))
	})

	t.Run("untrusted client setting X-Forwarded-Proto https is ignored", func(t *testing.T) {
		assert.False(t, runRequest(t, trusted, "203.0.113.10:54321", "https"))
	})

	t.Run("trusted proxy with X-Forwarded-Proto http stays insecure", func(t *testing.T) {
		assert.False(t, runRequest(t, trusted, "127.0.0.1:54321", "http"))
	})

	t.Run("no trusted proxies configured ignores header even from loopback", func(t *testing.T) {
		assert.False(t, runRequest(t, nil, "127.0.0.1:54321", "https"))
	})

	t.Run("unparseable remote addr is untrusted", func(t *testing.T) {
		assert.False(t, runRequest(t, trusted, "garbage", "https"))
	})
}

// TestProxiedEnvironmentRoutesAreEdgeRoutable fails when a route the manager forwards to remote environments is
// missing from the edge command allowlist, so edge environments cannot silently lose an endpoint.
func TestProxiedEnvironmentRoutesAreEdgeRoutable(t *testing.T) {
	type proxiedRoute struct {
		method string
		path   string
		stream bool
	}
	var routes []proxiedRoute
	for path, item := range api.SetupAPIForSpec().OpenAPI().Paths {
		operations := map[string]*huma.Operation{
			http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPut: item.Put,
			http.MethodDelete: item.Delete, http.MethodPatch: item.Patch, http.MethodHead: item.Head,
		}
		for method, operation := range operations {
			if operation != nil {
				routes = append(routes, proxiedRoute{method: method, path: path})
			}
		}
	}

	cfg := &config.Config{AgentMode: true, AppUrl: "http://localhost:3552", Environment: config.AppEnvironmentTest}
	router, _ := newRouter(RouterParams{
		Context: t.Context(),
		Config:  cfg,
		HandlerDeps: api.HandlerDeps{
			Project:   project.New(&project.ProjectService{}, nil),
			Container: container.New(&container.ContainerService{}, nil, nil, nil),
			Swarm:     swarm.New(&swarm.SwarmService{}, nil, nil, nil),
			System:    system.New(&system.SystemService{}, nil, nil),
			Health:    health.New(&health.Service{}),
			Settings:  newTestSettingsModule(t, cfg),
		},
		AuthMiddleware: auth.NewAuthMiddleware(nil, cfg),
		TunnelRegistry: edge.NewTunnelRegistry(),
	})
	for _, route := range router.Router().Routes() {
		if strings.HasPrefix(route.Path, "/api/environments/:id/ws/") && !strings.HasSuffix(route.Path, "*") {
			routes = append(routes, proxiedRoute{method: route.Method, path: strings.TrimPrefix(route.Path, "/api"), stream: true})
		}
	}

	concrete := strings.NewReplacer("{", "", "}", "", ":", "")
	var missing []string
	for _, route := range routes {
		suffix, ok := strings.CutPrefix(route.path, "/environments/{id}")
		if !ok {
			suffix, ok = strings.CutPrefix(route.path, "/environments/:id")
		}
		if !ok || suffix == "" || middleware.IsManagerLocalPath(route.method, suffix) {
			continue
		}
		if edge.ResolveEdgeCommandName(route.method, "/api/environments/0"+concrete.Replace(suffix), route.stream).IsAbsent() {
			missing = append(missing, route.method+" /api/environments/{id}"+suffix)
		}
	}

	slices.Sort(missing)
	assert.Empty(t, slices.Compact(missing),
		"proxied environment routes missing from the edge command allowlist; add them to pkg/libarcane/edge/commands.go or classify them in middleware.IsManagerLocalPath")
}

func newAgentTestRouter(t *testing.T) (*echo.Echo, huma.API) {
	t.Helper()
	cfg := &config.Config{AgentMode: true, AppUrl: "http://localhost:3552", Environment: config.AppEnvironmentTest}
	router, _ := newRouter(RouterParams{
		Context: t.Context(),
		Config:  cfg,
		HandlerDeps: api.HandlerDeps{
			Project:   project.New(&project.ProjectService{}, nil),
			Container: container.New(&container.ContainerService{}, nil, nil, nil),
			Swarm:     swarm.New(&swarm.SwarmService{}, nil, nil, nil),
			System:    system.New(&system.SystemService{}, nil, nil),
			Health:    health.New(&health.Service{}),
			Settings:  newTestSettingsModule(t, cfg),
		},
		AuthMiddleware: auth.NewAuthMiddleware(nil, cfg),
		TunnelRegistry: edge.NewTunnelRegistry(),
	})
	specRouter := echo.New()
	agentAPI := api.SetupAPI(specRouter, specRouter.Group("/api"), handlerutil.NewActivityAppContext(t.Context()), cfg, api.HandlerDeps{Health: health.New(&health.Service{})})
	return router, agentAPI
}

func operationsByMethod(item *huma.PathItem) map[string]*huma.Operation {
	return map[string]*huma.Operation{
		http.MethodGet:    item.Get,
		http.MethodPost:   item.Post,
		http.MethodPut:    item.Put,
		http.MethodDelete: item.Delete,
		http.MethodPatch:  item.Patch,
		http.MethodHead:   item.Head,
	}
}

func TestEchoRoutesDeclared(t *testing.T) {
	router, agentAPI := newAgentTestRouter(t)
	humaRoutes := map[string]struct{}{}
	echoPath := strings.NewReplacer("{", ":", "}", "")
	for path, item := range agentAPI.OpenAPI().Paths {
		for method, op := range operationsByMethod(item) {
			if op != nil {
				humaRoutes[method+" /api"+echoPath.Replace(path)] = struct{}{}
			}
		}
	}
	matcher := authz.NewPermissionMatcher()
	ws.AddProxiedPermissions(matcher)
	declaredEchoRoutes := map[string]string{
		"POST /api/webhooks/trigger/:token":  "public; the URL token is the credential",
		"POST /api/auth/federated/token":     "public RFC 8693 token exchange",
		"POST /api/events":                   "agent event ingestion authenticated by the environment access token",
		"GET /api/docs":                      "public API reference page",
		"GET /api/health/ready":              "public readiness probe hidden from the OpenAPI spec",
		"GET /api/diagnostics/stream":        authz.PermDiagnosticsRead,
		"GET /api/diagnostics/actors/stream": authz.PermDiagnosticsRead,
		"GET /api/diagnostics/logs/stream":   authz.PermDiagnosticsRead,
		"GET /api/debug/pprof":               authz.PermDiagnosticsRead,
		"GET /api/debug/pprof/":              authz.PermDiagnosticsRead,
		"GET /api/debug/pprof/cmdline":       authz.PermDiagnosticsRead,
		"GET /api/debug/pprof/profile":       authz.PermDiagnosticsRead,
		"GET /api/debug/pprof/symbol":        authz.PermDiagnosticsRead,
		"POST /api/debug/pprof/symbol":       authz.PermDiagnosticsRead,
		"GET /api/debug/pprof/trace":         authz.PermDiagnosticsRead,
		"GET /api/debug/pprof/:name":         authz.PermDiagnosticsRead,
		"GET /api/auth/autologin":            "autologin build feature",
		"POST /api/auth/autologin":           "autologin build feature",
		"POST /api/playwright/gitops":        "playwright build tag, non-production only",
		"POST /api/playwright/apikeys":       "playwright build tag, non-production only",
		"DELETE /api/playwright/apikeys":     "playwright build tag, non-production only",
		"POST /api/playwright/federated":     "playwright build tag, non-production only",
		"POST /api/tunnel/poll":              "edge tunnel authenticated by the agent token",
		"POST /api/tunnel/mtls/enroll":       "edge tunnel authenticated by the agent token",
		"GET /api/tunnel/connect":            "edge tunnel authenticated by the agent token",
	}

	var undeclared []string
	for _, route := range router.Router().Routes() {
		if !strings.HasPrefix(route.Path, "/api/") || route.Path == "/api/*" || route.Method == echo.RouteNotFound ||
			strings.HasPrefix(route.Path, "/api/openapi") || strings.HasPrefix(route.Path, "/api/schemas") {
			continue
		}
		key := route.Method + " " + route.Path
		if _, ok := humaRoutes[key]; ok {
			continue
		}
		if _, ok := declaredEchoRoutes[key]; ok {
			continue
		}
		if suffix, ok := strings.CutPrefix(route.Path, "/api/environments/:id"); ok && matcher.Lookup(route.Method, suffix, nil).IsPresent() {
			continue
		}
		undeclared = append(undeclared, key)
	}

	slices.Sort(undeclared)
	assert.Empty(t, slices.Compact(undeclared),
		"Echo routes bypass Huma permission metadata; declare their access control here or register a proxied permission in api/ws")
}

func TestEveryCatalogPermissionIsEnforced(t *testing.T) {
	router, agentAPI := newAgentTestRouter(t)
	enforced := map[string]struct{}{}
	for _, humaAPI := range []huma.API{api.SetupAPIForSpec(), agentAPI} {
		for _, item := range humaAPI.OpenAPI().Paths {
			for _, op := range operationsByMethod(item) {
				if op == nil {
					continue
				}
				if permission, ok := op.Metadata[authz.MetaRequiredPermission].(string); ok {
					enforced[permission] = struct{}{}
				}
				actions, _ := op.Metadata[authz.MetaActionPermissions].(map[string]string)
				for _, permission := range actions {
					enforced[permission] = struct{}{}
				}
			}
		}
	}
	matcher := authz.NewPermissionMatcher()
	ws.AddProxiedPermissions(matcher)
	for _, route := range router.Router().Routes() {
		if suffix, ok := strings.CutPrefix(route.Path, "/api/environments/:id"); ok {
			if permission, found := matcher.Lookup(route.Method, suffix, nil).Get(); found {
				enforced[permission] = struct{}{}
			}
		}
	}
	for _, kind := range []string{types.UploadKindImage, types.UploadKindVolumeBackup, types.UploadKindBuildWorkspace} {
		permission, ok := authz.UploadKindPermission(kind)
		require.True(t, ok, kind)
		enforced[permission] = struct{}{}
	}
	handlerEnforcedPermissions := map[string]string{
		authz.PermGitOpsLifecycle:  "gitops handler and the environment proxy require it when a request body configures a pre-deploy hook",
		authz.PermEnvironmentsList: "the environment list handler filters results by it",
		authz.PermDiagnosticsRead:  "Echo diagnostics streams and pprof routes",
	}
	accessSurfaceOnlyPermissions := map[string]string{
		authz.PermCustomizeManage: "gates customize navigation surfaces; customize resources enforce their own permissions",
	}

	var missing []string
	for _, permission := range authz.AllPermissions() {
		_, isEnforced := enforced[permission]
		_, isHandlerEnforced := handlerEnforcedPermissions[permission]
		_, isSurfaceOnly := accessSurfaceOnlyPermissions[permission]
		if !isEnforced && !isHandlerEnforced && !isSurfaceOnly {
			missing = append(missing, permission)
		}
	}

	assert.Empty(t, missing, "catalog permissions with no enforcement point; enforce them on a route or declare where they are checked")
}
