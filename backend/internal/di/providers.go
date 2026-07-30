package di

import (
	"context"
	"embed"
	"log/slog"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	arcanelogging "github.com/getarcaneapp/arcane/backend/v2/internal/logging"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"go.uber.org/fx"
)

func provideResourcesFSInternal() embed.FS {
	return resources.FS
}

func provideDockerClientServiceInternal(ctx context.Context, lc fx.Lifecycle, db *database.DB, cfg *config.Config, settings *services.SettingsService) *services.DockerClientService {
	service := services.NewDockerClientService(ctx, db, cfg, settings)
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go service.WatchEvents(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			service.Close()
			return nil
		},
	})
	return service
}

func provideVersionServiceInternal(httpClient *http.Client, cfg *config.Config, registry *services.ContainerRegistryService, docker *services.DockerClientService, imageUpdate *services.ImageUpdateService) *services.VersionService {
	return services.NewVersionService(httpClient, cfg.UpdateCheckDisabled, config.Version, config.Revision, registry, docker, imageUpdate)
}

func provideGitRepositoryServiceInternal(db *database.DB, cfg *config.Config, event *services.EventService, settings *services.SettingsService) *services.GitRepositoryService {
	return services.NewGitRepositoryService(db, cfg.GitWorkDir, event, settings)
}

func provideVolumeServiceInternal(lc fx.Lifecycle, db *database.DB, docker *services.DockerClientService, event *services.EventService, settings *services.SettingsService, container *services.ContainerService, image *services.ImageService, cfg *config.Config) *services.VolumeService {
	service := services.NewVolumeService(db, docker, event, settings, container, image, cfg.BackupVolumeName)
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			service.CleanupHelperContainers(ctx)
			return nil
		},
	})
	return service
}

func provideAuthServiceInternal(user *services.UserService, settings *services.SettingsService, event *services.EventService, session *services.SessionService, role *services.RoleService, cfg *config.Config, errorHandler *arcanelogging.SlogErrorHandler) *services.AuthService {
	return services.NewAuthService(user, settings, event, session, role, cfg.JWTSecret, cfg, errorHandler)
}

func provideContainerRegistryServiceInternal(db *database.DB, docker *services.DockerClientService, kv *services.KVService) *services.ContainerRegistryService {
	return services.NewContainerRegistryService(db, func(ctx context.Context) (services.RegistryDaemonClient, error) {
		return docker.GetClient(ctx)
	}, kv)
}

func provideProjectServiceInternal(db *database.DB, settings *services.SettingsService, event *services.EventService, image *services.ImageService, docker *services.DockerClientService, build *services.BuildService, lifecycle *services.LifecycleService, kv *services.KVService, registry *services.ContainerRegistryService, environment *services.EnvironmentService, cfg *config.Config) *services.ProjectService {
	return services.NewProjectService(db, settings, event, image, docker, build, lifecycle, registry, cfg).
		WithKVService(kv).
		WithRegistryCredentialsProvider(environment.GetEnabledRegistryCredentials)
}

// updaterServiceParams collects the updater's dependencies. The dedicated
// provider exists because NewUpdaterService takes its self-upgrade dependency
// as an unexported interface, which fx cannot supply directly; the adapter
// narrows *SystemUpgradeService to that interface.
type updaterServiceParams struct {
	fx.In

	DB            *database.DB
	Settings      *services.SettingsService
	Docker        *services.DockerClientService
	Project       *services.ProjectService
	ImageUpdate   *services.ImageUpdateService
	Registry      *services.ContainerRegistryService
	Event         *services.EventService
	Image         *services.ImageService
	Notification  *services.NotificationService
	SystemUpgrade *services.SystemUpgradeService
	Activity      *services.ActivityService
}

func provideUpdaterServiceInternal(p updaterServiceParams) (*services.UpdaterService, error) {
	return services.NewUpdaterService(p.DB, p.Settings, p.Docker, p.Project, p.ImageUpdate, p.Registry, p.Event, p.Image, p.Notification, p.SystemUpgrade, p.Activity)
}

func provideUserServiceInternal(db *database.DB, role *services.RoleService) *services.UserService {
	return services.NewUserService(db).WithRoleService(role)
}

func provideApiKeyServiceInternal(db *database.DB, user *services.UserService, role *services.RoleService) *services.ApiKeyService {
	return services.NewApiKeyService(db, user).WithRoleService(role)
}

func provideFederatedCredentialServiceInternal(db *database.DB, auth *services.AuthService, user *services.UserService, settings *services.SettingsService, event *services.EventService, httpClient *http.Client, role *services.RoleService) *services.FederatedCredentialService {
	return services.NewFederatedCredentialService(db, auth, user, settings, event, httpClient).WithRoleService(role)
}

func provideAuthMiddlewareInternal(auth *services.AuthService, apiKey *services.ApiKeyService, env *services.EnvironmentService, role *services.RoleService, cfg *config.Config) *middleware.AuthMiddleware {
	return middleware.NewAuthMiddleware(auth, cfg).
		WithApiKeyValidator(apiKey).
		WithEnvironmentAccessTokenResolver(env).
		WithPermissionResolver(role)
}

func provideFilesystemWatcherJobInternal(ctx context.Context, project *services.ProjectService, template *services.TemplateService, settings *services.SettingsService, cfg *config.Config) *scheduler.FilesystemWatcherJob {
	job, err := scheduler.RegisterFilesystemWatcherJob(ctx, project, template, settings, cfg.ProjectScanMaxDepth)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to register filesystem watcher job", "error", err)
	}
	return job
}
