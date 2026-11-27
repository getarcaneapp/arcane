package bootstrap

import (
	"context"
	"log/slog"
	"net"
	"slices"
	"strings"

	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"
	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/api"
	"getarcane.dev/arcane/v3/api/ws"
	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/middleware"
	"getarcane.dev/arcane/v3/internal/role"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/webhook"
	"getarcane.dev/arcane/v3/pkg/agent"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/arcane/v3/pkg/cookie"
	"getarcane.dev/arcane/v3/pkg/handlerutil"
	"getarcane.dev/arcane/v3/pkg/httpx"
	"getarcane.dev/arcane/v3/pkg/startup"
	"getarcane.dev/types/v3"
)

type (
	RouterParams struct {
		fx.In

		Context        context.Context
		Lifecycle      fx.Lifecycle
		Config         *config.Config
		HandlerDeps    api.HandlerDeps
		ManagerDeps    api.ManagerHandlerDeps
		AuthMiddleware *auth.AuthMiddleware
		TunnelRegistry *agent.TunnelRegistry
		TunnelServer   *agent.TunnelServer
	}

	initializeManagerStartupStateParams struct {
		fx.In

		AppCtx context.Context
		Config *config.Config
		DB     *database.DB

		Settings    *settings.SettingsService
		Environment *environment.EnvironmentService
		Role        *role.RoleService
		User        *user.UserService
		ApiKey      *apikey.ApiKeyService
	}
)

func managerOptions(agentEnabled bool) fx.Option {
	if agentEnabled {
		return fx.Options()
	}
	return fx.Invoke(registerWebhookLifecycle, initializeManagerStartupState, registerManagerJobs)
}

func registerWebhookLifecycle(lc fx.Lifecycle, service *webhook.WebhookService) {
	lc.Append(fx.Hook{
		OnStart: service.LoadTokenHashes,
		OnStop: func(ctx context.Context) error {
			if err := service.DrainActions(ctx); err != nil {
				slog.WarnContext(ctx, "shutdown proceeding with webhook actions still in flight", "error", err)
			}
			return nil
		},
	})
}

func routerProvider(agentEnabled bool) any {
	if agentEnabled {
		return newAgentRouter
	}
	return newRouter
}

func createAuthValidator(deps api.HandlerDeps, mgrDeps api.ManagerHandlerDeps) middleware.AuthValidator {
	resolveUser := func(ctx context.Context, user *types.Actor) *authz.PermissionSet {
		ps, err := deps.RoleService.ResolvePermissions(ctx, user.ID)
		if err != nil || ps == nil {
			slog.WarnContext(ctx, "failed to resolve user permissions for env proxy", "error", err)
			return authz.NewPermissionSet()
		}
		return ps
	}
	resolveKey := func(ctx context.Context, keyID string) *authz.PermissionSet {
		ps, err := deps.RoleService.ResolveApiKeyPermissions(ctx, keyID)
		if err != nil || ps == nil {
			slog.WarnContext(ctx, "failed to resolve api key permissions for env proxy", "error", err)
			return authz.NewPermissionSet()
		}
		return ps
	}
	return func(ctx context.Context, c *echo.Context) (*authz.PermissionSet, *types.Actor, bool) {
		req := c.Request()
		if apiKey := req.Header.Get(middleware.HeaderApiKey); apiKey != "" {
			key, err := deps.ApiKeyService.ValidateRawApiKey(ctx, apiKey)
			if err != nil {
				return nil, nil, false
			}
			if key.UserID != nil {
				u, userErr := mgrDeps.User.Service().GetUserByID(ctx, *key.UserID)
				if userErr != nil || u.Disabled {
					return nil, nil, false
				}
				if key.Kind != apikey.ApiKeyKindPersonal {
					return resolveKey(ctx, key.ID), u.Actor(), true
				}
				return resolveUser(ctx, u.Actor()), u.Actor(), true
			}
			if key.EnvironmentID != nil {
				return authz.EnvironmentPermissionSet(*key.EnvironmentID), nil, true
			}
			return nil, nil, false
		}

		verify := deps.AuthService.VerifyBrowserToken
		token, err := cookie.GetTokenCookie(req)
		if bearer, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer "); ok {
			verify, token, err = deps.AuthService.VerifyToken, bearer, nil
		}
		if err != nil || token == "" {
			return nil, nil, false
		}
		u, _, err := verify(ctx, token)
		if err != nil || u == nil || deps.AuthService.RequirePasswordChanged(u, "") != nil {
			return nil, nil, false
		}
		return resolveUser(ctx, u.Actor()), u.Actor(), true
	}
}

func newRouter(p RouterParams) *echo.Echo {
	e, trustedProxyNets := newBaseRouter(p.Context, p.Config, p.HandlerDeps)

	e.Use(echomiddleware.SecureWithConfig(echomiddleware.SecureConfig{
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "SAMEORIGIN",
		ContentSecurityPolicy: "frame-ancestors 'self'; base-uri 'self'; object-src 'none'",
	}))
	e.Use(secureCookieContextMiddleware(trustedProxyNets))
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			secure, _ := c.Request().Context().Value(cookie.SecureCookieContextKey{}).(bool)
			if secure && p.HandlerDeps.Settings.Service().GetSettingsConfig().HstsEnabled.IsTrue() {
				c.Response().Header().Set(echo.HeaderStrictTransportSecurity, "max-age=31536000")
			}
			return next(c)
		}
	})
	e.Use(middleware.WarnUntrustedForwardedFor(trustedProxyNets))
	e.Use(middleware.NewCORSMiddleware(p.Config).Add())
	e.Use(middleware.NewCSRFMiddleware(p.Config).Add())

	apiGroup := e.Group("/api")
	apiGroup.Use(middleware.NewCompressionMiddleware())

	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{
			"/api/auth/login",
			"/api/auth/password",
			"/api/auth/me/passkeys/reauth/password",
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
	apiGroup.Use(middleware.PerIPRateLimitForPaths(
		[]string{"/api/agents/enroll"}, 30, 10,
	))
	apiGroup.Use(middleware.PerTokenRateLimitForPaths(
		[]string{"/api/webhooks/trigger/:token"}, 60, 10,
		func(c *echo.Context) string { return c.Param("token") },
		p.ManagerDeps.Webhook.Service().IsKnownToken,
	))
	apiGroup.Use(middleware.PerTokenRateLimitForPaths(
		[]string{"/api/events"}, 600, 2*event.DaemonEventBatchSize,
		func(c *echo.Context) string {
			return strings.TrimSpace(c.Request().Header.Get(middleware.HeaderAgentToken))
		},
		p.HandlerDeps.EnvironmentService.IsKnownAccessToken,
	))

	envResolver := func(ctx context.Context, id string) (*string, bool, error) {
		env, err := p.HandlerDeps.EnvironmentService.GetEnvironmentByIDCached(ctx, id)
		if err != nil {
			return nil, false, err
		}
		return env.AccessToken, env.Enabled, nil
	}

	permissionMatcher := authz.NewPermissionMatcher()

	envProxyMiddleware := middleware.NewEnvironmentProxyMiddleware(middleware.EnvironmentProxyConfig{
		LocalID:       types.LocalDockerEnvironmentID,
		ParamName:     "id",
		Resolver:      envResolver,
		AuthValidator: createAuthValidator(p.HandlerDeps, p.ManagerDeps),
		Matcher:       permissionMatcher,
		Registry:      p.TunnelRegistry,
		CheckOrigin:   httpx.ValidateWebSocketOrigin(p.Config.GetAppURL()),
	})
	apiGroup.Use(middleware.NewEnvironmentScopeMiddleware(func(ctx context.Context, id string) (types.EnvironmentRef, error) {
		env, err := p.HandlerDeps.EnvironmentService.GetEnvironmentByIDCached(ctx, id)
		if err != nil {
			return types.EnvironmentRef{}, err
		}
		return types.EnvironmentRef{ID: id, Name: environment.DisplayName(id, env.Name)}, nil
	}, permissionMatcher, func(ctx context.Context) int {
		return p.HandlerDeps.Settings.Service().GetIntSetting(ctx, "allEnvironmentsLimit")
	}))
	apiGroup.Use(envProxyMiddleware)

	middleware.RegisterEchoRoutes(apiGroup, tunnelRoutes(p.TunnelServer))

	humaAPI := api.SetupAPI(api.SetupParams{
		Echo:        e,
		Group:       apiGroup,
		AppContext:  handlerutil.NewActivityAppContext(p.Context),
		Config:      p.Config,
		Deps:        p.HandlerDeps,
		ManagerDeps: p.ManagerDeps,
		Tunnel:      p.TunnelServer,
	})

	permissionMatcher.CollectFromHumaAPI(humaAPI)
	ws.AddProxiedPermissions(permissionMatcher)

	return finishRouter(e, apiGroup, AgentRouterParams{
		Context:        p.Context,
		Lifecycle:      p.Lifecycle,
		Config:         p.Config,
		HandlerDeps:    p.HandlerDeps,
		AuthMiddleware: p.AuthMiddleware,
		TunnelRegistry: p.TunnelRegistry,
	})
}

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

func initializeManagerStartupState(p initializeManagerStartupStateParams) error {
	if err := p.Environment.ReconcileAgentStatusesOnStartup(p.AppCtx); err != nil {
		slog.WarnContext(p.AppCtx, "Failed to reconcile agent environment statuses on startup", "error", err)
	}
	startup.InitializeNonAgentFeatures(p.AppCtx, p.Config.AgentEnabled,
		p.Role.EnsureBuiltInRoles,
		func(ctx context.Context) error {
			if err := p.Role.BackfillLegacyRoleAssignments(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to backfill legacy role assignments before admin bootstrap", "error", err)
			}
			if p.Config.DefaultAdminDisabled {
				return nil
			}
			return p.User.CreateDefaultAdmin(ctx, user.DefaultAdminOptions{
				Username:       p.Config.DefaultAdminUsername,
				Password:       p.Config.DefaultAdminPassword,
				PasswordPolicy: p.Settings.GetSettingsConfig().PasswordPolicy(),
				DataDirectory:  "data",
			})
		},
		func(ctx context.Context) error {
			return p.ApiKey.ReconcileDefaultAdminAPIKey(ctx, p.Config.DefaultAdminUsername, p.Config.AdminStaticAPIKey)
		},
		func(ctx context.Context) error {
			startup.InitializeAutoLogin(ctx, p.Config.AutoLoginUsername)
			return nil
		},
	)
	if err := p.ApiKey.BackfillApiKeyPermissions(p.AppCtx); err != nil {
		slog.WarnContext(p.AppCtx, "Failed to backfill API key permissions", "error", err)
	}
	if err := p.Role.ReconcileEnvOidcMappings(p.AppCtx, p.Config.OidcRoleMappings); err != nil {
		slog.ErrorContext(p.AppCtx, "Failed to reconcile ARCANE_OIDC_ROLE_MAPPINGS", "error", err)
	}
	if err := p.Role.AssertGlobalAdminExists(p.AppCtx, p.DB.WithContext(p.AppCtx)); err != nil {
		slog.ErrorContext(p.AppCtx, "RBAC global admin guard failed", "error", err)
	}
	return nil
}
