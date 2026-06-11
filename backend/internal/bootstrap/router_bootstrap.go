package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"

	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/getarcaneapp/arcane/backend/v2/api/handlers"
	"github.com/getarcaneapp/arcane/backend/v2/api/ws"
	"github.com/getarcaneapp/arcane/backend/v2/frontend"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/di"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/types/v2"
)

var (
	registerPlaywrightRoutes []func(apiGroup *echo.Group, services *di.Services)
	registerBuildableRoutes  []func(apiGroup *echo.Group, services *di.Services)
)

var loggerSkipPatterns = []string{
	"POST /api/tunnel/poll",
	"GET /api/environments/*/ws/containers/*/logs",
	"GET /api/environments/*/ws/containers/*/stats",
	"GET /api/environments/*/ws/containers/*/terminal",
	"GET /api/environments/*/ws/projects/*/logs",
	"GET /api/environments/*/ws/system/stats",
	"GET /_app/*",
	"GET /img",
	"GET /api/health",
	"HEAD /api/health",
	// Static branding / PWA assets — browsers re-request these frequently
	// and the logs add no signal.
	"GET /api/app-images/*",
}

func shouldLogRequestInternal(c echo.Context) bool {
	mp := c.Request().Method + " " + c.Request().URL.Path
	for _, pat := range loggerSkipPatterns {
		if pat == mp {
			return false
		}
		if before, ok := strings.CutSuffix(pat, "/*"); ok {
			if strings.HasPrefix(mp, before) {
				return false
			}
		}
		if ok, _ := path.Match(pat, mp); ok {
			return false
		}
		if strings.HasSuffix(pat, "/") && strings.HasPrefix(mp, pat) {
			return false
		}
	}
	return true
}

// requestLoggerMiddlewareInternal wraps slog-echo and filters out internal
// edge tunnel requests plus high-volume endpoints (health, WS, static).
func requestLoggerMiddlewareInternal() echo.MiddlewareFunc {
	loggerMiddleware := slogecho.NewWithConfig(slog.Default(), slogecho.Config{
		Filters: []slogecho.Filter{shouldLogRequestInternal},
	})

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if edge.IsInternalTunnelRequest(c.Request().Context()) {
				return next(c)
			}
			if c.Request().Body == nil {
				c.Request().Body = http.NoBody
			}
			return loggerMiddleware(next)(c)
		}
	}
}

func createAuthValidatorInternal(appServices *di.Services) middleware.AuthValidator {
	return func(ctx context.Context, c echo.Context) bool {
		req := c.Request()
		// Check for API key authentication
		if apiKey := req.Header.Get("X-Api-Key"); apiKey != "" {
			// User-owned API key
			if user, err := appServices.ApiKey.ValidateApiKey(ctx, apiKey); err == nil && user != nil {
				return true
			}
			// Environment bootstrap key (user_id = NULL): used by the proxy when forwarding
			// requests to a remote env whose apiUrl resolves back to this manager.
			if _, err := appServices.ApiKey.GetEnvironmentByApiKey(ctx, apiKey); err == nil {
				return true
			}
			return false
		}

		// Check for Bearer token authentication
		token := ""
		if auth := req.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if cookieToken, err := cookie.GetTokenCookie(req); err == nil && cookieToken != "" {
			token = cookieToken
		}

		if token == "" {
			return false
		}

		user, _, err := appServices.Auth.VerifyToken(ctx, token)
		return err == nil && user != nil
	}
}

func setupRouter(ctx context.Context, cfg *config.Config, appServices *di.Services) (*echo.Echo, *edge.TunnelServer) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	trustedProxyNets := parseTrustedProxyCIDRsInternal(cfg.TrustedProxies)
	if cfg.TrustedProxies != "" && len(trustedProxyNets) == 0 {
		slog.Warn("TRUSTED_PROXIES set but no valid CIDRs found; falling back to direct IP extraction")
	}
	if len(trustedProxyNets) == 0 {
		e.IPExtractor = echo.ExtractIPDirect()
	} else {
		opts := make([]echo.TrustOption, 0, len(trustedProxyNets))
		for _, ipnet := range trustedProxyNets {
			opts = append(opts, echo.TrustIPRange(ipnet))
		}
		e.IPExtractor = echo.ExtractIPFromXFFHeader(opts...)
	}

	e.Use(echomiddleware.Recover())
	e.Use(requestLoggerMiddlewareInternal())                       //nolint:contextcheck
	e.Use(secureCookieContextMiddlewareInternal(trustedProxyNets)) //nolint:contextcheck

	authMiddleware := appServices.AuthMiddleware
	e.Use(middleware.NewCORSMiddleware(cfg).Add())
	e.Use(middleware.NewCSRFMiddleware(cfg).Add()) //nolint:contextcheck // Echo middleware uses request context from echo.Context, not the app lifecycle context.

	apiGroup := e.Group("/api")

	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{
			"/api/auth/login",
			"/api/auth/refresh",
			"/api/oidc/callback",
		}, 5, 5,
	))
	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{"/api/auth/federated/token"}, 10, 10,
	))
	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{"/api/webhooks/trigger/:token"}, 60, 10,
	))
	handlerAppCtx := handlers.NewActivityAppContext(ctx)

	tunnelRegistry := edge.NewTunnelRegistry()
	edge.SetDefaultRegistry(tunnelRegistry)
	envResolver := func(ctx context.Context, id string) (string, *string, bool, error) {
		env, err := appServices.Environment.GetEnvironmentByID(ctx, id)
		if err != nil || env == nil {
			return "", nil, false, err
		}
		return env.ApiUrl, env.AccessToken, env.Enabled, nil
	}

	// Register public webhook trigger endpoint before auth middleware (token in URL is the sole auth)
	api.RegisterWebhookTrigger(apiGroup, appServices.Webhook, handlerAppCtx) //nolint:contextcheck // app lifecycle context is intentionally wrapped for detached activity work.
	handlers.RegisterFederatedTokenExchange(apiGroup, appServices.Federated) //nolint:contextcheck // public RFC 8693 form endpoint uses request context.
	handlers.RegisterAgentEventIngestion(apiGroup, appServices.Event, cfg)   //nolint:contextcheck // internal agent-token route; intentionally outside user/RBAC auth.

	//nolint:contextcheck // Echo middleware reads context from echo.Context.Request().Context(), not a parameter.
	envProxyMiddleware := middleware.NewEnvProxyMiddlewareWithParam(
		types.LOCAL_DOCKER_ENVIRONMENT_ID,
		"id",
		envResolver,
		createAuthValidatorInternal(appServices),
	)
	apiGroup.Use(envProxyMiddleware)

	_ = api.SetupAPI(e, apiGroup, handlerAppCtx, cfg, appServices) //nolint:contextcheck // app lifecycle context is intentionally wrapped for detached activity work.

	for _, register := range registerBuildableRoutes {
		register(apiGroup, appServices)
	}

	// Remaining echo handlers (WebSocket/streaming)
	ws.NewWebSocketHandler(apiGroup, appServices.Project, appServices.Container, appServices.Swarm, appServices.System, appServices.Diagnostics, authMiddleware, cfg) //nolint:contextcheck

	// Register edge tunnel endpoint for manager to accept agent connections
	// This is only registered when NOT in agent mode (i.e., running as manager)
	var tunnelServer *edge.TunnelServer
	if !cfg.AgentMode {
		tunnelServer = registerEdgeTunnelRoutes(ctx, cfg, apiGroup, appServices)
	}

	if cfg.Environment != "production" {
		for _, registerFunc := range registerPlaywrightRoutes {
			registerFunc(apiGroup, appServices)
		}
	}

	if err := frontend.RegisterFrontend(e); err != nil {
		if errors.Is(err, frontend.ErrFrontendNotIncluded) {
			slog.Debug("Frontend not included in this build; skipping frontend registration")
		} else {
			slog.Error("Failed to register frontend", "error", err)
		}
	}

	return e, tunnelServer
}

// parseTrustedProxyCIDRsInternal parses TRUSTED_PROXIES into a list of
// validated networks. Invalid entries are logged and skipped.
func parseTrustedProxyCIDRsInternal(raw string) []*net.IPNet {
	if raw == "" {
		return nil
	}
	var nets []*net.IPNet
	for cidr := range strings.SplitSeq(raw, ",") {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			slog.Warn("invalid TRUSTED_PROXIES CIDR, ignoring", "cidr", cidr, "error", err)
			continue
		}
		nets = append(nets, ipnet)
	}
	return nets
}

// secureCookieContextMiddlewareInternal records whether the request should be
// treated as HTTPS for cookie-emitting handlers. X-Forwarded-Proto is honored
// ONLY when the direct TCP peer is in TRUSTED_PROXIES — an untrusted client
// setting the header directly cannot trick the server into issuing Secure /
// __Host- cookies over plain HTTP.
func secureCookieContextMiddlewareInternal(trustedProxyNets []*net.IPNet) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			secure := req.TLS != nil
			if !secure && len(trustedProxyNets) > 0 &&
				strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https") &&
				remoteAddrInTrustedProxiesInternal(req.RemoteAddr, trustedProxyNets) {
				secure = true
			}
			if secure {
				c.SetRequest(req.WithContext(cookie.WithSecureCookieContext(req.Context(), true)))
			}
			return next(c)
		}
	}
}

// remoteAddrInTrustedProxiesInternal reports whether the direct TCP peer
// address of the request falls within any of the configured trusted-proxy
// networks. Unparseable remote addresses are treated as untrusted.
func remoteAddrInTrustedProxiesInternal(remoteAddr string, nets []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
