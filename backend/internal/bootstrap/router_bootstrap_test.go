package bootstrap

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humaecho"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/api"
	"github.com/getarcaneapp/arcane/backend/v3/frontend"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/health"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/cookie"
)

func TestNewRouter_DecodesPathParams(t *testing.T) {
	cfg := &config.Config{
		AgentEnabled: true,
		AppUrl:       "http://localhost:3552",
		Environment:  config.AppEnvironmentTest,
	}
	router, _, err := newRouter(RouterParams{
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
		TunnelRegistry: agent.NewTunnelRegistry(),
	})
	require.NoError(t, err)
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

// TestProxiedEnvironmentRoutesAreAgentRoutable fails when a route the manager forwards to remote environments is
// missing from the agent command allowlist, so agent environments cannot silently lose an endpoint.
func TestProxiedEnvironmentRoutesAreAgentRoutable(t *testing.T) {
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

	cfg := &config.Config{AgentEnabled: true, AppUrl: "http://localhost:3552", Environment: config.AppEnvironmentTest}
	router, _, err := newRouter(RouterParams{
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
		TunnelRegistry: agent.NewTunnelRegistry(),
	})
	require.NoError(t, err)
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
		if agent.ResolveCommandName(route.method, "/api/environments/0"+concrete.Replace(suffix), route.stream).IsAbsent() {
			missing = append(missing, route.method+" /api/environments/{id}"+suffix)
		}
	}

	slices.Sort(missing)
	assert.Empty(t, slices.Compact(missing),
		"proxied environment routes missing from the agent command allowlist; add them to pkg/libarcane/agent/commands.go or classify them in middleware.IsManagerLocalPath")
}

func TestBasePathMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		target         string
		wantPath       string
		wantRawPath    string
		wantRequestURI string
	}{
		{name: "prefixed", target: "/arcane/api/health?x=1", wantPath: "/api/health", wantRequestURI: "/api/health?x=1"},
		{name: "base only", target: "/arcane", wantPath: "/", wantRequestURI: "/"},
		{name: "base with slash", target: "/arcane/", wantPath: "/", wantRequestURI: "/"},
		{name: "base with query", target: "/arcane?x=1", wantPath: "/", wantRequestURI: "/?x=1"},
		{name: "unprefixed passes through", target: "/api/health", wantPath: "/api/health", wantRequestURI: "/api/health"},
		{name: "similar prefix is not the base", target: "/arcane2/api", wantPath: "/arcane2/api", wantRequestURI: "/arcane2/api"},
		{name: "encoded percent is not decoded twice", target: "/arcane/api/volumes/a%252Fb", wantPath: "/api/volumes/a%2Fb", wantRequestURI: "/api/volumes/a%252Fb"},
		{name: "encoded slash keeps its raw form", target: "/arcane/api/volumes/a%2Fb", wantPath: "/api/volumes/a/b", wantRawPath: "/api/volumes/a%2Fb", wantRequestURI: "/api/volumes/a%2Fb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			e.Pre(basePathMiddleware("/arcane"))
			var got *http.Request
			e.Any("/*", func(c *echo.Context) error {
				got = c.Request()
				return c.NoContent(http.StatusNoContent)
			})
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, http.NoBody))
			require.NotNil(t, got)
			assert.Equal(t, tt.wantPath, got.URL.Path)
			assert.Equal(t, tt.wantRawPath, got.URL.RawPath)
			assert.Equal(t, tt.wantRequestURI, got.RequestURI)
		})
	}
}

func TestBasePathMiddlewareWithCSRF(t *testing.T) {
	cfg := &config.Config{Environment: "production", AppUrl: "https://example.com/arcane"}
	e := echo.New()
	e.Pre(basePathMiddleware("/arcane"))
	e.Use(middleware.NewCSRFMiddleware(cfg).Add())
	e.POST("/api/test", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	e.POST("/api/webhooks/trigger/:token", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

	post := func(target, site string) int {
		req := httptest.NewRequest(http.MethodPost, target, http.NoBody)
		req.Header.Set("Sec-Fetch-Site", site)
		if site == "cross-site" {
			req.Header.Set("Origin", "https://evil.example.com")
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}
	assert.Equal(t, http.StatusForbidden, post("/arcane/api/test", "cross-site"))
	assert.Equal(t, http.StatusOK, post("/arcane/api/test", "same-origin"))
	assert.Equal(t, http.StatusOK, post("/arcane/api/webhooks/trigger/abc", "cross-site"))
}

func TestBasePathMiddlewareWithFrontend(t *testing.T) {
	e := echo.New()
	e.Pre(basePathMiddleware("/arcane"))
	e.GET("/api/health", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })
	err := frontend.RegisterFrontend(e, "/arcane")
	if errors.Is(err, frontend.ErrFrontendNotIncluded) {
		t.Skip("frontend not included in this build")
	}
	require.NoError(t, err)

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, http.NoBody))
		return rec
	}
	for _, target := range []string{"/arcane/api/health", "/api/health"} {
		assert.Equal(t, http.StatusOK, get(target).Code, target)
	}
	for _, target := range []string{"/arcane/", "/arcane/containers", "/containers"} {
		rec := get(target)
		assert.Equal(t, http.StatusOK, rec.Code, target)
		assert.Contains(t, rec.Body.String(), `base: "/arcane"`, target)
		assert.Contains(t, rec.Body.String(), `location.replace(`, target)
	}
	assert.Contains(t, get("/arcanefoo/api/health").Body.String(), `base: "/arcane"`)
}
