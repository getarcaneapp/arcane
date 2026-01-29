package bootstrap

import (
	"context"
	"log/slog"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	sloggin "github.com/samber/slog-gin"

	"github.com/getarcaneapp/arcane/backend/frontend"
	"github.com/getarcaneapp/arcane/backend/internal/api"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/huma"
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/internal/utils/cookie"
	"github.com/getarcaneapp/arcane/backend/internal/utils/edge"
	"github.com/getarcaneapp/arcane/types"
)

var registerPlaywrightRoutes []func(apiGroup *gin.RouterGroup, services *Services)

var loggerSkipPatterns = []string{
	"GET /api/environments/*/ws/containers/*/logs",
	"GET /api/environments/*/ws/containers/*/stats",
	"GET /api/environments/*/ws/containers/*/terminal",
	"GET /api/environments/*/ws/projects/*/logs",
	"GET /api/environments/*/ws/system/stats",
	"GET /_app/*",
	"GET /img",
	"GET /api/fonts/sans",
	"GET /api/fonts/mono",
	"GET /api/fonts/serif",
	"GET /api/health",
	"HEAD /api/health",
}

func shouldLogRequest(c *gin.Context) bool {
	mp := c.Request.Method + " " + c.Request.URL.Path
	for _, pat := range loggerSkipPatterns {
		if pat == mp {
			return false
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "/*")
			if strings.HasPrefix(mp, prefix) {
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

func createAuthValidator(appServices *Services) middleware.AuthValidator {
	return func(ctx context.Context, c *gin.Context) bool {
		// Check for API key authentication
		if apiKey := c.GetHeader("X-API-Key"); apiKey != "" {
			user, err := appServices.ApiKey.ValidateApiKey(ctx, apiKey)
			return err == nil && user != nil
		}

		// Check for Bearer token authentication
		token := ""
		if auth := c.GetHeader("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if cookieToken, err := cookie.GetTokenCookie(c); err == nil && cookieToken != "" {
			token = cookieToken
		}

		if token == "" {
			return false
		}

		user, err := appServices.Auth.VerifyToken(ctx, token)
		return err == nil && user != nil
	}
}

func setupRouter(ctx context.Context, cfg *config.Config, appServices *Services) (*gin.Engine, *edge.TunnelServer) {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())

	router.Use(sloggin.NewWithConfig(slog.Default(), sloggin.Config{
		Filters: []sloggin.Filter{shouldLogRequest},
	}))

	authMiddleware := middleware.NewAuthMiddleware(appServices.Auth, cfg).WithApiKeyValidator(appServices.ApiKey)
	corsMiddleware := middleware.NewCORSMiddleware(cfg).Add()
	router.Use(corsMiddleware)

	apiGroup := router.Group("/api")

	envMiddleware := middleware.NewEnvProxyMiddlewareWithParam(
		types.LOCAL_DOCKER_ENVIRONMENT_ID,
		"id",
		func(ctx context.Context, id string) (string, *string, bool, error) {
			env, err := appServices.Environment.GetEnvironmentByID(ctx, id)
			if err != nil || env == nil {
				return "", nil, false, err
			}
			return env.ApiUrl, env.AccessToken, env.Enabled, nil
		},
		appServices.Environment,
		createAuthValidator(appServices),
	)
	apiGroup.Use(envMiddleware)

	_ = huma.SetupAPI(router, apiGroup, cfg, &huma.Services{
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
		Volume:            appServices.Volume,
		Container:         appServices.Container,
		Network:           appServices.Network,
		Notification:      appServices.Notification,
		Apprise:           appServices.Apprise,
		Updater:           appServices.Updater,
		CustomizeSearch:   appServices.CustomizeSearch,
		System:            appServices.System,
		SystemUpgrade:     appServices.SystemUpgrade,
		GitRepository:     appServices.GitRepository,
		GitOpsSync:        appServices.GitOpsSync,
		Config:            cfg,
	})

	// Remaining Gin handlers (WebSocket/streaming)
	api.NewWebSocketHandler(apiGroup, appServices.Project, appServices.Container, appServices.System, authMiddleware, cfg) //nolint:contextcheck

	// Register edge tunnel endpoint for manager to accept agent connections
	// This is only registered when NOT in agent mode (i.e., running as manager)
	var tunnelServer *edge.TunnelServer
	if !cfg.AgentMode {
		tunnelServer = registerEdgeTunnelRoutes(ctx, apiGroup, appServices)
	}

	if cfg.Environment != "production" {
		for _, registerFunc := range registerPlaywrightRoutes {
			registerFunc(apiGroup, appServices)
		}
	}

	if err := frontend.RegisterFrontend(router); err != nil {
		_, _ = gin.DefaultErrorWriter.Write([]byte("Failed to register frontend: " + err.Error() + "\n"))
	}

	return router, tunnelServer
}
