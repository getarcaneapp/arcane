package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"

	echootel "github.com/labstack/echo-otel/v5"
	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
	slogecho "github.com/samber/slog-echo/v2"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/api"
	"getarcane.dev/arcane/v3/api/ws"
	"getarcane.dev/arcane/v3/frontend"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
)

type AgentRouterParams struct {
	fx.In

	Context        context.Context
	Lifecycle      fx.Lifecycle
	Config         *config.Config
	HandlerDeps    api.HandlerDeps
	AuthMiddleware *auth.AuthMiddleware
	TunnelRegistry *agent.TunnelRegistry
}

var loggerSkipPatterns = []string{
	"GET /api/agents/*/ws/containers/stats",
	"GET /api/agents/*/ws/containers/*/logs",
	"GET /api/agents/*/ws/containers/*/stats",
	"GET /api/agents/*/ws/containers/*/terminal",
	"GET /api/agents/*/ws/projects/*/logs",
	"GET /api/agents/*/ws/system/stats",
	"GET /api/stream",
	"POST /api/telemetry/*",
	"GET /_app/*",
	"GET /img",
	"GET /api/health",
	"HEAD /api/health",
	"GET /api/app/images/*",
	"PUT /api/agents/*/uploads/*/*/chunks/*",
}

func newAgentRouter(p AgentRouterParams) *echo.Echo {
	e, _ := newBaseRouter(p.Context, p.Config, p.HandlerDeps)

	apiGroup := e.Group("/api")

	api.SetupAPI(api.SetupParams{
		Echo:       e,
		Group:      apiGroup,
		AppContext: handlerutil.NewActivityAppContext(p.Context),
		Config:     p.Config,
		Deps:       p.HandlerDeps,
	})

	return finishRouter(e, apiGroup, p)
}

func newBaseRouter(ctx context.Context, cfg *config.Config, deps api.HandlerDeps) (*echo.Echo, []*net.IPNet) {
	// Path params are decoded after matching on the escaped path, so an encoded "/" cannot change the route.
	e := echo.NewWithConfig(echo.Config{
		Router: echo.NewRouter(echo.RouterConfig{AllowOverwritingRoute: true, UnescapePathParamValues: true}),
	})

	var trustedProxyNets []*net.IPNet
	// Trust only the configured ranges, not Echo's built-in loopback/link-local/private defaults.
	trustOpts := []echo.TrustOption{echo.TrustLoopback(false), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false)}
	for cidr := range strings.SplitSeq(cfg.TrustedProxies, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			slog.WarnContext(ctx, "invalid ARCANE_TRUSTED_PROXIES CIDR, ignoring", "cidr", cidr, "error", err)
			continue
		}
		trustedProxyNets = append(trustedProxyNets, ipnet)
		trustOpts = append(trustOpts, echo.TrustIPRange(ipnet))
	}
	if cfg.TrustedProxies != "" && len(trustedProxyNets) == 0 {
		slog.WarnContext(ctx, "ARCANE_TRUSTED_PROXIES set but no valid CIDRs found; falling back to direct IP extraction")
	}
	if len(trustedProxyNets) == 0 {
		e.IPExtractor = echo.ExtractIPDirect()
	} else {
		e.IPExtractor = echo.ExtractIPFromXFFHeader(trustOpts...)
	}

	e.Use(echomiddleware.Recover())
	e.Use(echomiddleware.RequestID())
	e.Use(middleware.RejectDotSegmentParams)
	// Trace REST API calls only, skipping long-lived streams, tunnels, health probes, and the browser trace relay.
	e.Use(echootel.NewMiddlewareWithConfig(echootel.Config{Skipper: func(c *echo.Context) bool {
		req := c.Request()
		p := req.URL.Path
		return !strings.HasPrefix(p, "/api/") || p == "/api/health" ||
			strings.HasPrefix(p, "/api/telemetry/") || strings.HasPrefix(p, "/api/tunnel/") ||
			httpx.IsWebSocketUpgradeRequest(req) || strings.Contains(req.Header.Get("Accept"), "text/event-stream")
	}}))
	e.Use(middleware.SpanEnvironment(deps.EnvironmentService.ResolveEnvironmentName, environment.DisplayName))
	requestLogger := slogecho.NewWithConfig(slog.Default(), slogecho.Config{
		DefaultLevel:     slog.LevelInfo,
		ClientErrorLevel: slog.LevelWarn,
		ServerErrorLevel: slog.LevelError,
		Filters: []slogecho.Filter{func(c *echo.Context, _ error) bool {
			mp := c.Request().Method + " " + c.Request().URL.Path
			return !slices.ContainsFunc(loggerSkipPatterns, func(pat string) bool {
				if prefix, ok := strings.CutSuffix(pat, "/*"); ok && strings.HasPrefix(mp, prefix) {
					return true
				}
				matched, _ := path.Match(pat, mp)
				return matched
			})
		}},
		WithRequestID: true,
		WithTraceID:   true,
		WithSpanID:    true,
	})
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		logged := requestLogger(next)
		return func(c *echo.Context) error {
			if c.Request().Body == nil {
				c.Request().Body = http.NoBody
			}
			return logged(c)
		}
	})

	return e, trustedProxyNets
}

func finishRouter(e *echo.Echo, apiGroup *echo.Group, p AgentRouterParams) *echo.Echo {
	ws.NewWebSocketHandler(ws.HandlerParams{
		AppCtx:             p.Context,
		Group:              apiGroup,
		ProjectService:     p.HandlerDeps.Project.Service(),
		ContainerService:   p.HandlerDeps.Container.Service(),
		SwarmService:       p.HandlerDeps.Swarm.Service(),
		SystemService:      p.HandlerDeps.System.Service(),
		DiagnosticsService: p.HandlerDeps.Diagnostics,
		AuthMiddleware:     p.AuthMiddleware,
		AuthService:        p.HandlerDeps.AuthService,
		SettingsService:    p.HandlerDeps.Settings.Service(),
		Config:             p.Config,
	})

	apiNotFound := func(c *echo.Context) error {
		return c.JSON(http.StatusNotFound, map[string]any{
			"success": false,
			"error":   "API endpoint not found: " + c.Request().URL.Path,
		})
	}
	apiGroup.RouteNotFound("", apiNotFound)
	apiGroup.RouteNotFound("/*", apiNotFound)

	//nolint:staticcheck,nolintlint // SA4023 only under exclude_frontend: the stub always returns ErrFrontendNotIncluded
	if err := frontend.RegisterFrontend(e); err != nil {
		if errors.Is(err, frontend.ErrFrontendNotIncluded) {
			slog.DebugContext(p.Context, "Frontend not included in this build; skipping frontend registration")
		} else {
			slog.ErrorContext(p.Context, "Failed to register frontend", "error", err)
		}
	}

	return e
}

func EchoRoutePermissions() []authz.PermissionRoute {
	permissionRoutes := api.SpecRoutes()
	for _, route := range slices.Concat(tunnelRoutes(nil), ws.Routes()) {
		permissionRoutes = append(permissionRoutes, route.PermissionRoute)
	}
	return permissionRoutes
}
