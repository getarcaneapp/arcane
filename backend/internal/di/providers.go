package di

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
	"go.uber.org/fx"
)

func provideActivityModuleInternal(service *activity.ActivityService, environment *environment.EnvironmentService) *activity.Module {
	return activity.New(service, activity.EnvironmentDependencies{
		ProxyJSONRequest:               environment.ProxyJSONRequest,
		ListActiveRemoteEnvironments:   environment.ListActiveRemoteEnvironments,
		GetActiveRemoteEnvironment:     environment.GetActiveRemoteEnvironmentSnapshot,
		ProxyJSONRequestForEnvironment: environment.ProxyJSONRequestForEnvironment,
		ResolveEnvironmentName:         environment.ResolveEnvironmentName,
	})
}

func provideImageUpdateModuleInternal(service *imageupdate.ImageUpdateService, imageService *image.ImageService) *imageupdate.Module {
	return imageupdate.New(service, imageService.GetUpdateInfoByImageRefs)
}

func provideSettingsModuleInternal(service *settings.SettingsService, search *settings.SettingsSearchService, environment *environment.EnvironmentService, cfg *config.Config) *settings.Module {
	return settings.New(service, search, environment.ProxyJSONRequest, cfg)
}

func provideBackupEngineInternal(ctx context.Context, lc fx.Lifecycle, admission *runs.Admission, imageService *image.ImageService, runtime *francis.Runtime, coordinator *runs.Coordinator, roles *role.RoleService, cfg *config.Config) (*backup.Engine, error) {
	engine := backup.NewEngine(ctx, admission, imageService)
	if err := engine.Register(runtime); err != nil {
		return nil, err
	}
	engine.SetExecutionReady(coordinator.Active)
	engine.SetAuthorize(func(ctx context.Context, command backuptypes.DurableRunCommand) error {
		if cfg.AgentMode && command.UserID == "agent" && command.RequestedWithKey == "" {
			return nil
		}
		if command.UserID == "" {
			return errors.New("requesting user unavailable")
		}
		permissions, err := roles.ResolveExecutionPermissions(ctx, command.UserID, command.RequestedWithKey)
		if err != nil {
			return err
		}
		if !permissions.Allows(command.Permission, command.EnvironmentID) {
			return errors.New("requesting user no longer has permission for this backup")
		}
		if command.Permission == authz.PermSystemBackupsManage && !permissions.IsGlobalAdmin() {
			return errors.New("system backups require a global administrator")
		}
		return nil
	})
	lc.Append(fx.Hook{OnStop: engine.Stop})
	return engine, nil
}

func provideActorRuntimeInternal(appCtx context.Context, cfg *config.Config, settingsService *settings.SettingsService, lc fx.Lifecycle, cancelApp context.CancelFunc) (*francis.Runtime, error) {
	runtime, err := francis.New(cfg.DatabaseURL, "", "", cfg.ActorPort)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if err := runtime.ConfigureIdentity(cfg.EncryptionKey, settingsService.GetSettingsConfig().InstanceID.Value); err != nil {
			return err
		}
		return runtime.Start(ctx, appCtx, func(err error) { slog.ErrorContext(appCtx, "Francis host failed", "error", err); cancelApp() })
	}, OnStop: runtime.Stop})
	return runtime, nil
}

func provideRunCoordinatorInternal(runtime *francis.Runtime, store *kv.KVService, cfg *config.Config) (*runs.Coordinator, error) {
	coordinator := runs.New(store, runtime.Service(), cfg.GetLocation())
	if err := coordinator.Register(runtime); err != nil {
		return nil, err
	}
	return coordinator, nil
}

func provideAdmissionGateInternal(runtime *francis.Runtime) (*runs.Admission, error) {
	admission := runs.NewAdmission(runtime.Service(), uuid.New().String())
	return admission, admission.Register(runtime)
}

func provideTunnelRegistryInternal(lc fx.Lifecycle) *edge.TunnelRegistry {
	registry := edge.NewTunnelRegistry()
	edge.SetDefaultRegistry(registry)
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error { defer edge.ClearDefaultRegistry(registry); return registry.Stop(ctx) }})
	return registry
}

func provideDockerClientServiceInternal(ctx context.Context, lc fx.Lifecycle, db *database.DB, cfg *config.Config, settings *settings.SettingsService, eventService *event.EventService) *docker.DockerClientService {
	service := docker.NewDockerClientService(ctx, db, cfg, settings, bus.WithDroppedEventCallback(func(message events.Message) {
		// Overflow is recovered synchronously, applying backpressure instead of losing log entries.
		eventService.RecordDockerEvent(ctx, message)
	}))
	var stopStream, stopLog func(context.Context) error
	var unsubscribe func()
	lc.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			run, cleanup := eventService.SubscribeDockerEvents(service.EventBus())
			unsubscribe = cleanup
			var err error
			stopLog, err = concurrency.StartSupervised(ctx, "Docker event log", run)
			if err != nil {
				cleanup()
				return err
			}
			stopStream, err = concurrency.StartSupervised(ctx, "Docker event watcher", func(runCtx context.Context) error {
				service.WatchEvents(runCtx)
				return nil
			})
			if err != nil {
				err = errors.Join(err, stopLog(startCtx))
				cleanup()
			}
			return err
		},
		OnStop: func(stopCtx context.Context) error {
			var err error
			if stopStream != nil {
				err = stopStream(stopCtx)
			}
			if stopLog != nil {
				err = errors.Join(err, stopLog(stopCtx))
			}
			if unsubscribe != nil {
				unsubscribe()
			}
			service.Close()
			return err
		},
	})
	return service
}

func provideVersionServiceInternal(httpClient *http.Client, cfg *config.Config, registry *registry.ContainerRegistryService, docker *docker.DockerClientService, imageUpdate *imageupdate.ImageUpdateService, settingsService *settings.SettingsService) *version.VersionService {
	return version.NewVersionService(httpClient, cfg.UpdateCheckDisabled, config.Version, config.Revision, registry, docker, imageUpdate, settingsService)
}

func provideGitRepositoryServiceInternal(db *database.DB, cfg *config.Config, eventService *event.EventService, settingsService *settings.SettingsService) *gitrepo.GitRepositoryService {
	return gitrepo.NewGitRepositoryService(db, cfg.GitWorkDir, eventService, settingsService)
}

func provideS3ModuleInternal(service *s3domain.S3DestinationService, environment *environment.EnvironmentService) *s3domain.Module {
	return s3domain.New(service, environment.SyncS3DestinationsToRemoteEnvironments)
}

func provideS3ServiceInternal(db *database.DB, environment *environment.EnvironmentService) *s3domain.S3DestinationService {
	return s3domain.NewS3DestinationService(db, environment.CheckS3DestinationReferences)
}

func provideAuthModuleInternal(service *auth.AuthService, userService *user.UserService, settingsService *settings.SettingsService, passkeyService *passkey.PasskeyService) *auth.Module {
	return auth.New(service, userService, settingsService, passkeyService.BeginMFAAuthentication)
}

func provideContainerRegistryServiceInternal(db *database.DB, docker *docker.DockerClientService, kv *kv.KVService, settings *settings.SettingsService) *registry.ContainerRegistryService {
	return registry.NewContainerRegistryService(db, func(ctx context.Context) (registry.RegistryDaemonClient, error) { return docker.GetClient(ctx) }, kv, settings)
}

func provideContainerRegistryModuleInternal(service *registry.ContainerRegistryService, environment *environment.EnvironmentService) *registry.Module {
	return registry.New(service, environment.SyncRegistriesToRemoteEnvironments)
}

func provideProjectServiceInternal(db *database.DB, settings *settings.SettingsService, event *event.EventService, image *image.ImageService, docker *docker.DockerClientService, build *build.BuildService, lifecycleService *project.LifecycleService, kv *kv.KVService, registry *registry.ContainerRegistryService, environment *environment.EnvironmentService, cfg *config.Config) *project.ProjectService {
	return project.NewProjectService(db, settings, event, image, docker, build, lifecycleService, registry, cfg, kv, environment.GetEnabledRegistryCredentials)
}

// updaterServiceParams includes actor registration and worker lifecycle dependencies.
type updaterServiceParams struct {
	fx.In

	Context       context.Context
	Config        *config.Config
	Lifecycle     fx.Lifecycle
	ActorRuntime  *francis.Runtime
	Coordinator   *runs.Coordinator
	Admission     *runs.Admission
	Roles         *role.RoleService
	DB            *database.DB
	Settings      *settings.SettingsService
	Docker        *docker.DockerClientService
	Project       *project.ProjectService
	ImageUpdate   *imageupdate.ImageUpdateService
	Registry      *registry.ContainerRegistryService
	Event         *event.EventService
	Image         *image.ImageService
	Notification  *notification.NotificationService
	SystemUpgrade *system.SystemUpgradeService
	Activity      *activity.ActivityService
}

func provideUpdaterServiceInternal(p updaterServiceParams) (*updater.UpdaterService, error) {
	service, err := updater.NewUpdaterService(p.DB, p.Settings, p.Docker, p.Project, p.ImageUpdate, p.Registry, p.Event, p.Image, p.Notification, p.SystemUpgrade, p.Activity, p.Config, p.Coordinator, p.Admission, p.Roles)
	if err != nil {
		return nil, err
	}
	if err := service.RegisterActors(p.ActorRuntime); err != nil {
		return nil, err
	}
	p.Lifecycle.Append(fx.Hook{OnStart: func(context.Context) error { return service.Start(p.Context) }, OnStop: service.Stop}) //nolint:contextcheck // Workers inherit the application lifetime after startup returns.
	return service, nil
}

func provideUserModuleInternal(service *user.UserService, auth *auth.AuthService, settingsService *settings.SettingsService) *user.Module {
	return user.New(service, auth.InvalidateUserTokenCache, settingsService)
}

func provideJWKSetManagerInternal(ctx context.Context, lc fx.Lifecycle) *oidcjwk.KeySetManager {
	manager := oidcjwk.NewKeySetManager(ctx)
	lc.Append(fx.Hook{OnStop: manager.Shutdown})
	return manager
}

func provideAuthMiddlewareInternal(authService *auth.AuthService, apiKey *apikey.ApiKeyService, env *environment.EnvironmentService, role *role.RoleService, cfg *config.Config) *auth.AuthMiddleware {
	return auth.NewAuthMiddleware(authService, cfg).
		WithApiKeyValidator(apiKey).
		WithEnvironmentAccessTokenResolver(env).
		WithPermissionResolver(role)
}

func provideFilesystemWatcherJobInternal(ctx context.Context, lc fx.Lifecycle, project *project.ProjectService, template *template.TemplateService, settings *settings.SettingsService, cfg *config.Config) (*scheduler.FilesystemWatcherJob, error) {
	job, err := scheduler.NewFilesystemWatcherJob(ctx, project, template, settings, cfg.ProjectScanMaxDepth)
	if err != nil {
		return nil, err
	}
	var stop func(context.Context) error
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			stop, err = concurrency.StartSupervised(ctx, "filesystem watcher", func(runCtx context.Context) error {
				delay := 5 * time.Second
				for runCtx.Err() == nil {
					startErr := job.Start(runCtx)
					if startErr == nil {
						break
					}
					slog.WarnContext(runCtx, "Filesystem watcher startup failed; retrying", "error", startErr)

					timer := time.NewTimer(delay)
					select {
					case <-runCtx.Done():
						timer.Stop()
						return nil
					case <-timer.C:
					}
					delay = min(delay*2, 5*time.Minute)
				}
				<-runCtx.Done()
				return nil
			})
			if err != nil {
				return err
			}
			slog.InfoContext(ctx, "Filesystem watcher job registered")
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			var err error
			if stop != nil {
				err = stop(stopCtx)
			}
			return errors.Join(err, job.Stop(stopCtx))
		},
	})
	return job, nil
}
