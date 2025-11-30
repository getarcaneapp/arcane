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
	"github.com/getarcaneapp/arcane/backend/internal/middleware"
)

var registerPlaywrightRoutes []func(apiGroup *gin.RouterGroup, services *Services)

func setupRouter(cfg *config.Config, appServices *Services) *gin.Engine {

	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())

	loggerSkipPatterns := []string{
		"GET /api/environments/*/containers/*/logs/ws",
		"GET /api/environments/*/containers/*/stats/ws",
		"GET /api/environments/*/system/stats/ws",
		"GET /api/environments/*/projects/*/logs/ws",
		"GET /api/environments/*/containers/*/exec/ws",
		"GET /_app/*",
		"GET /img",
		"GET /fonts",
		"GET /api/health",
		"HEAD /api/health",
	}

	router.Use(sloggin.NewWithConfig(slog.Default(), sloggin.Config{
		Filters: []sloggin.Filter{
			func(c *gin.Context) bool {
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
			},
		},
	}))

	authMiddleware := middleware.NewAuthMiddleware(appServices.Auth, cfg).WithApiKeyValidator(appServices.ApiKey)
	corsMiddleware := middleware.NewCORSMiddleware(cfg).Add()
	router.Use(corsMiddleware)

	apiGroup := router.Group("/api")

	api.NewApplicationImagesHandler(apiGroup, appServices.AppImages)
	api.NewUserHandler(apiGroup, appServices.User, authMiddleware)
	api.NewVersionHandler(apiGroup, appServices.Version)
	api.NewAuthHandler(apiGroup, appServices.User, appServices.Auth, appServices.Oidc, authMiddleware)
	api.NewEventHandler(apiGroup, appServices.Event, authMiddleware)
	api.NewOidcHandler(apiGroup, appServices.Auth, appServices.Oidc)
	api.NewEnvironmentHandler(apiGroup, appServices.Environment, appServices.Settings, authMiddleware, cfg)
	api.NewContainerRegistryHandler(apiGroup, appServices.ContainerRegistry, authMiddleware)
	api.NewTemplateHandler(apiGroup, appServices.Template, authMiddleware)
	api.NewApiKeyHandler(apiGroup, appServices.ApiKey, authMiddleware)

	envMiddleware := middleware.NewEnvProxyMiddlewareWithParam(
		api.LOCAL_DOCKER_ENVIRONMENT_ID,
		"id",
		func(ctx context.Context, id string) (string, *string, bool, error) {
			env, err := appServices.Environment.GetEnvironmentByID(ctx, id)
			if err != nil || env == nil {
				return "", nil, false, err
			}
			return env.ApiUrl, env.AccessToken, env.Enabled, nil
		},
		appServices.Environment,
	)
	apiGroup.Use(envMiddleware)

	api.NewHealthHandler(apiGroup)
	api.NewContainerHandler(apiGroup, appServices.Docker, appServices.Container, appServices.Image, authMiddleware, cfg)
	api.NewImageHandler(apiGroup, appServices.Docker, appServices.Image, appServices.ImageUpdate, appServices.Settings, authMiddleware)
	api.NewImageUpdateHandler(apiGroup, appServices.ImageUpdate, authMiddleware)
	api.NewNetworkHandler(apiGroup, appServices.Docker, appServices.Network, authMiddleware)
	api.NewProjectHandler(apiGroup, appServices.Project, authMiddleware, cfg)
	api.NewSystemHandler(apiGroup, appServices.Docker, appServices.System, appServices.SystemUpgrade, authMiddleware, cfg)
	api.NewUpdaterHandler(apiGroup, appServices.Updater, authMiddleware)
	api.NewVolumeHandler(apiGroup, appServices.Docker, appServices.Volume, authMiddleware)
	api.NewNotificationHandler(apiGroup, appServices.Notification, appServices.Apprise, authMiddleware)
	api.NewSettingsHandler(apiGroup, appServices.Settings, appServices.SettingsSearch, authMiddleware)
	api.NewCustomizeHandler(apiGroup, appServices.CustomizeSearch, authMiddleware)

	if cfg.Environment != "production" {
		for _, registerFunc := range registerPlaywrightRoutes {
			registerFunc(apiGroup, appServices)
		}
	}

	if err := frontend.RegisterFrontend(router); err != nil {
		_, _ = gin.DefaultErrorWriter.Write([]byte("Failed to register frontend: " + err.Error() + "\n"))
	}

	return router
}
