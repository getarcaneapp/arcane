package bootstrap

import (
	"context"
	"log/slog"
	"net"
	"path"
	"strings"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	slogecho "github.com/samber/slog-echo"

	"github.com/getarcaneapp/arcane/backend/api"
	"github.com/getarcaneapp/arcane/backend/api/ws"
	"github.com/getarcaneapp/arcane/backend/frontend"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/pkg/utils/cookie"
	"github.com/getarcaneapp/arcane/types"
)

var (
	registerPlaywrightRoutes []func(apiGroup *echo.Group, services *Services)
	registerBuildableRoutes  []func(apiGroup *echo.Group, services *Services)
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
	"GET /api/fonts/sans",
	"GET /api/fonts/mono",
	"GET /api/health",
	"HEAD /api/health",
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
			return loggerMiddleware(next)(c)
		}
	}
}

func createAuthValidatorInternal(appServices *Services) middleware.AuthValidator {
	return func(ctx context.Context, c echo.Context) bool {
		req := c.Request()
		// Check for API key authentication
		if apiKey := req.Header.Get("X-API-Key"); apiKey != "" {
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

func setupRouter(ctx context.Context, cfg *config.Config, appServices *Services) (*echo.Echo, *edge.TunnelServer) {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	if cfg.TrustedProxies == "" {
		e.IPExtractor = echo.ExtractIPDirect()
	} else {
		var opts []echo.TrustOption
		for _, cidr := range strings.Split(cfg.TrustedProxies, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				slog.Warn("invalid TRUSTED_PROXIES CIDR, ignoring", "cidr", cidr, "error", err)
				continue
			}
			opts = append(opts, echo.TrustIPRange(ipnet))
		}
		if len(opts) == 0 {
			slog.Warn("TRUSTED_PROXIES set but no valid CIDRs found; falling back to direct IP extraction")
			e.IPExtractor = echo.ExtractIPDirect()
		} else {
			e.IPExtractor = echo.ExtractIPFromXFFHeader(opts...)
		}
	}

	e.Use(echomiddleware.Recover())
	e.Use(requestLoggerMiddlewareInternal()) //nolint:contextcheck

	authMiddleware := middleware.NewAuthMiddleware(appServices.Auth, cfg).
		WithApiKeyValidator(appServices.ApiKey).
		WithEnvironmentAccessTokenResolver(appServices.Environment)
	e.Use(middleware.NewCORSMiddleware(cfg).Add())

	apiGroup := e.Group("/api")

	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{
			"/api/auth/login",
			"/api/auth/refresh",
			"/api/oidc/callback",
		}, 5, 5,
	))
	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{"/api/webhooks/trigger/:token"}, 60, 10,
	))

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
	api.RegisterWebhookTrigger(apiGroup, appServices.Webhook) //nolint:contextcheck

	//nolint:contextcheck // Echo middleware reads context from echo.Context.Request().Context(), not a parameter.
	envProxyMiddleware := middleware.NewEnvProxyMiddlewareWithParam(
		types.LOCAL_DOCKER_ENVIRONMENT_ID,
		"id",
		envResolver,
		createAuthValidatorInternal(appServices),
	)
	apiGroup.Use(envProxyMiddleware)

	humaServices := &api.Services{
		User:              appServices.User,
		Auth:              appServices.Auth,
		Oidc:              appServices.Oidc,
		ApiKey:            appServices.ApiKey,
		AppImages:         appServices.AppImages,
		Font:              appServices.Font,
		Project:           appServices.Project,
		Event:             appServices.Event,
		Version:           appServices.Version,
		Environment:       appServices.Environment,
		Settings:          appServices.Settings,
		JobSchedule:       appServices.JobSchedule,
		SettingsSearch:    appServices.SettingsSearch,
		ContainerRegistry: appServices.ContainerRegistry,
		Template:          appServices.Template,
		Docker:            appServices.Docker,
		Image:             appServices.Image,
		ImageUpdate:       appServices.ImageUpdate,
		Build:             appServices.Build,
		BuildWorkspace:    appServices.BuildWorkspace,
		Volume:            appServices.Volume,
		Container:         appServices.Container,
		Network:           appServices.Network,
		Port:              appServices.Port,
		Swarm:             appServices.Swarm,
		Notification:      appServices.Notification,
		Apprise:           appServices.Apprise,
		Updater:           appServices.Updater,
		CustomizeSearch:   appServices.CustomizeSearch,
		System:            appServices.System,
		SystemUpgrade:     appServices.SystemUpgrade,
		GitRepository:     appServices.GitRepository,
		GitOpsSync:        appServices.GitOpsSync,
		Webhook:           appServices.Webhook,
		Vulnerability:     appServices.Vulnerability,
		Dashboard:         appServices.Dashboard,
		Config:            cfg,
	}

	_ = api.SetupAPI(e, apiGroup, cfg, humaServices)

	for _, register := range registerBuildableRoutes {
		register(apiGroup, appServices)
	}

	api.RegisterDiagnosticsRoutes(apiGroup, authMiddleware, ws.DefaultWebSocketMetrics()) //nolint:contextcheck

	// Remaining echo handlers (WebSocket/streaming)
	ws.NewWebSocketHandler(apiGroup, appServices.Project, appServices.Container, appServices.Swarm, appServices.System, authMiddleware, cfg) //nolint:contextcheck

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
		slog.Error("Failed to register frontend", "error", err)
	}

	return e, tunnelServer
}
