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

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo-otel/v5"
	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
	"github.com/samber/slog-echo/v2"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v3/api"
	"github.com/getarcaneapp/arcane/backend/v3/api/ws"
	"github.com/getarcaneapp/arcane/backend/v3/frontend"
	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/federated"
	"github.com/getarcaneapp/arcane/backend/v3/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v3/internal/telemetry"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/handlerutil"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
)

var (
	registerPlaywrightRoutes []func(apiGroup *echo.Group, deps api.HandlerDeps)
	registerBuildableRoutes  []func(apiGroup *echo.Group, deps api.HandlerDeps)
)

var loggerSkipPatterns = []string{
	"POST /api/tunnel/poll",
	"GET /api/environments/*/ws/containers/stats",
	"GET /api/environments/*/ws/containers/*/logs",
	"GET /api/environments/*/ws/containers/*/stats",
	"GET /api/environments/*/ws/containers/*/terminal",
	"GET /api/environments/*/ws/projects/*/logs",
	"GET /api/environments/*/ws/system/stats",
	"GET /api/stream",
	"POST /api/telemetry/*",
	"GET /_app/*",
	"GET /img",
	"GET /api/health",
	"HEAD /api/health",
	"GET /api/app/images/*",
	"PUT /api/environments/*/uploads/*/*/chunks/*",
}

func createAuthValidator(deps api.HandlerDeps) middleware.AuthValidator {
	resolveUser := func(ctx context.Context, user *types.Actor) *authz.PermissionSet {
		ps, err := deps.Role.Service().ResolvePermissions(ctx, user.ID)
		if err != nil || ps == nil {
			slog.WarnContext(ctx, "failed to resolve user permissions for env proxy", "error", err)
			return authz.NewPermissionSet()
		}
		return ps
	}
	resolveKey := func(ctx context.Context, keyID string) *authz.PermissionSet {
		ps, err := deps.Role.Service().ResolveApiKeyPermissions(ctx, keyID)
		if err != nil || ps == nil {
			slog.WarnContext(ctx, "failed to resolve api key permissions for env proxy", "error", err)
			return authz.NewPermissionSet()
		}
		return ps
	}
	return func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool) {
		req := c.Request()
		// Check for API key authentication
		if apiKey := req.Header.Get(middleware.HeaderApiKey); apiKey != "" {
			key, err := deps.ApiKey.Service().ValidateRawApiKey(ctx, apiKey)
			if err != nil {
				return nil, nil, false
			}
			// User-owned API key: personal keys inherit the owner's role
			// permissions; scoped keys are limited to their own grants.
			if key.UserID != nil {
				user, userErr := deps.User.Service().GetUserByID(ctx, *key.UserID)
				if userErr != nil {
					return nil, nil, false
				}
				if key.Kind != apikey.ApiKeyKindPersonal {
					return resolveKey(ctx, key.ID), user.Actor(), true
				}
				return resolveUser(ctx, user.Actor()), user.Actor(), true
			}
			// Environment bootstrap key (user_id = NULL): used by the proxy when forwarding
			// requests to a remote env whose apiUrl resolves back to this manager.
			if key.EnvironmentID != nil {
				return authz.EnvironmentPermissionSet(*key.EnvironmentID), nil, true
			}
			return nil, nil, false
		}

		// Bearer tokens take precedence over the browser token cookie.
		verify := deps.Auth.Service().VerifyBrowserToken
		token, err := cookie.GetTokenCookie(req)
		if bearer, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer "); ok {
			verify, token, err = deps.Auth.Service().VerifyToken, bearer, nil
		}
		if err != nil || token == "" {
			return nil, nil, false
		}
		user, _, err := verify(ctx, token)
		if err != nil || user == nil {
			return nil, nil, false
		}
		return resolveUser(ctx, user.Actor()), user.Actor(), true
	}
}

type RouterParams struct {
	fx.In

	Context        context.Context
	Lifecycle      fx.Lifecycle
	Config         *config.Config
	HandlerDeps    api.HandlerDeps
	AuthMiddleware *auth.AuthMiddleware
	TunnelRegistry *edge.TunnelRegistry
}

func newRouter(p RouterParams) (*echo.Echo, *edge.TunnelServer) {
	ctx := p.Context
	cfg := p.Config
	deps := p.HandlerDeps

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
	// Trace REST API calls only, skipping long-lived streams, tunnels, health probes, and the browser trace relay.
	e.Use(echootel.NewMiddlewareWithConfig(echootel.Config{Skipper: func(c *echo.Context) bool {
		req := c.Request()
		p := req.URL.Path
		return !strings.HasPrefix(p, "/api/") || p == "/api/health" ||
			strings.HasPrefix(p, "/api/telemetry/") || strings.HasPrefix(p, "/api/tunnel/") ||
			httpx.IsWebSocketUpgradeRequest(req) || strings.Contains(req.Header.Get("Accept"), "text/event-stream")
	}}))
	e.Use(middleware.SpanEnvironment(deps.Environment.Service().ResolveEnvironmentName))
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
	e.Use(secureCookieContextMiddleware(trustedProxyNets))
	e.Use(middleware.WarnUntrustedForwardedFor(trustedProxyNets))

	authMiddleware := p.AuthMiddleware
	e.Use(middleware.NewCORSMiddleware(cfg).Add())
	e.Use(middleware.NewCSRFMiddleware(cfg).Add())

	apiGroup := e.Group("/api")

	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{
			"/api/auth/login",
			"/api/auth/passkey/login/begin",
			"/api/auth/passkey/login/finish",
			"/api/auth/passkey/mobile/finish",
			"/api/auth/passkey/mobile/exchange",
			"/api/auth/mfa/passkey/begin",
			"/api/auth/mfa/passkey/finish",
			"/api/auth/mfa/recovery",
			"/api/oidc/callback",
		}, 5, 5,
	))
	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{"/api/auth/refresh"}, 30, 30,
	))
	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{"/api/auth/federated/token"}, 10, 10,
	))
	// Keyed per token for stored tokens, per source IP for everything else.
	apiGroup.Use(middleware.PerTokenRateLimitForPaths(
		[]string{"/api/webhooks/trigger/:token"}, 60, 10,
		func(c *echo.Context) string { return c.Param("token") },
		deps.Webhook.Service().IsKnownToken,
	))
	// Agent event ingestion sits outside the auth middleware, so it needs its own ceiling; agents batch events.
	apiGroup.Use(middleware.PerTokenRateLimitForPaths(
		[]string{"/api/events"}, 600, 2*event.DaemonEventBatchSize,
		func(c *echo.Context) string {
			return strings.TrimSpace(c.Request().Header.Get(middleware.HeaderAgentToken))
		},
		deps.Environment.Service().IsKnownAccessToken,
	))
	handlerAppCtx := handlerutil.NewActivityAppContext(ctx)

	envResolver := func(ctx context.Context, id string) (*string, bool, error) {
		env, err := deps.Environment.Service().GetEnvironmentByIDCached(ctx, id)
		if err != nil {
			return nil, false, err
		}
		return env.AccessToken, env.Enabled, nil
	}

	// Register public webhook trigger endpoint before auth middleware (token in URL is the sole auth)
	api.RegisterWebhookTrigger(apiGroup, deps.Webhook.Service(), handlerAppCtx)
	federated.RegisterFederatedTokenExchange(apiGroup, deps.Federated)
	if !cfg.EdgeAgent {
		telemetry.RegisterRoutes(apiGroup, telemetry.NewService())
	}
	deps.Event.RegisterAgentRoutes(apiGroup, func(ctx context.Context, token string) (string, error) {
		env, err := deps.Environment.Service().ResolveEnvironmentByAccessToken(ctx, token)
		if err != nil {
			return "", err
		}
		if env == nil || !env.Enabled || env.ID == "0" {
			return "", nil
		}
		return env.ID, nil
	})

	permissionMatcher := authz.NewPermissionMatcher()

	envProxyMiddleware := middleware.NewEnvProxyMiddlewareWithParamAndRegistry(
		types.LocalDockerEnvironmentID,
		"id",
		envResolver,
		createAuthValidator(deps),
		permissionMatcher,
		p.TunnelRegistry,
		httpx.ValidateWebSocketOrigin(cfg.GetAppURL()),
	)
	apiGroup.Use(middleware.NewEnvironmentScopeMiddleware(func(ctx context.Context, id string) (types.EnvironmentRef, error) {
		env, err := deps.Environment.Service().GetEnvironmentByIDCached(ctx, id)
		if err != nil {
			return types.EnvironmentRef{}, err
		}
		return types.EnvironmentRef{ID: id, Name: environment.DisplayName(id, env.Name)}, nil
	}, permissionMatcher, func(ctx context.Context) int {
		return deps.Settings.Service().GetIntSetting(ctx, "allEnvironmentsLimit")
	}))
	apiGroup.Use(envProxyMiddleware)

	humaAPI := api.SetupAPI(e, apiGroup, handlerAppCtx, cfg, deps)

	// Fill the proxy's shared matcher from the local API surface before serving so remote
	// requests are authorized with the same permissions.
	permissionMatcher.CollectFromHumaAPI(humaAPI)
	ws.AddProxiedPermissions(permissionMatcher)

	for _, register := range registerBuildableRoutes {
		register(apiGroup, deps)
	}

	// Remaining echo handlers (WebSocket/streaming)
	ws.NewWebSocketHandler(ctx, apiGroup, deps.Project.Service(), deps.Container.Service(), deps.Swarm.Service(), deps.System.Service(), deps.Diagnostics, authMiddleware, deps.Settings.Service(), cfg)

	// Managers accept agent connections on the edge tunnel endpoint.
	var tunnelServer *edge.TunnelServer
	if !cfg.EdgeAgent {
		tunnelServer = registerEdgeTunnelRoutes(ctx, p.Lifecycle, cfg, apiGroup, deps.Environment.Service(), deps.Event.Service(), deps.Notification.Service(), p.TunnelRegistry)
	}

	if cfg.Environment != "production" {
		for _, registerFunc := range registerPlaywrightRoutes {
			registerFunc(apiGroup, deps)
		}
	}

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
			slog.DebugContext(ctx, "Frontend not included in this build; skipping frontend registration")
		} else {
			slog.ErrorContext(ctx, "Failed to register frontend", "error", err)
		}
	}

	return e, tunnelServer
}

// secureCookieContextMiddleware marks requests as HTTPS for cookies. X-Forwarded-Proto is
// honored only when the direct TCP peer is in TRUSTED_PROXIES.
func secureCookieContextMiddleware(trustedProxyNets []*net.IPNet) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			secure := req.TLS != nil
			if !secure && len(trustedProxyNets) > 0 && strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") {
				host, _, err := net.SplitHostPort(req.RemoteAddr)
				if err != nil {
					host = req.RemoteAddr
				}
				// Unparseable remote addresses are untrusted.
				if ip := net.ParseIP(host); ip != nil {
					secure = slices.ContainsFunc(trustedProxyNets, func(n *net.IPNet) bool { return n.Contains(ip) })
				}
			}
			if secure {
				c.SetRequest(req.WithContext(context.WithValue(req.Context(), cookie.SecureCookieContextKey{}, true)))
			}
			return next(c)
		}
	}
}
