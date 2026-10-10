package di

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
)

func provideActivityModuleInternal(service *activity.ActivityService, localEnvironment *environment.EnvironmentService) *activity.Module {
	return activity.New(service, activity.EnvironmentDependencies{
		ProxyJSONRequest:               localEnvironment.ProxyJSONRequest,
		ListActiveRemoteEnvironments:   localEnvironment.ListActiveRemoteEnvironments,
		GetActiveRemoteEnvironment:     localEnvironment.GetActiveRemoteEnvironmentSnapshot,
		ProxyJSONRequestForEnvironment: localEnvironment.ProxyJSONRequestForEnvironment,
		ResolveEnvironmentName:         localEnvironment.ResolveEnvironmentName,
	})
}

func provideImageUpdateModuleInternal(service *imageupdate.ImageUpdateService, imageService *image.ImageService) *imageupdate.Module {
	return imageupdate.New(service, imageService.GetUpdateInfoByImageRefs)
}

func provideSettingsModuleInternal(service *settings.SettingsService, search *settings.SettingsSearchService, localEnvironment *environment.EnvironmentService, cfg *config.Config) *settings.Module {
	return settings.New(service, search, localEnvironment.ProxyJSONRequest, cfg)
}

func provideBackupEngineInternal(lc fx.Lifecycle, admission *runs.Admission, imageService *image.ImageService, roles *role.RoleService, cfg *config.Config) *backup.Engine {
	engine := backup.NewEngine(admission, imageService)
	engine.SetAuthorize(func(ctx context.Context, requester backuptypes.Requester) error {
		if cfg.AgentMode && requester.UserID == "agent" && requester.APIKeyID == "" {
			return nil
		}
		if requester.UserID == "" {
			return errors.New("requesting user unavailable")
		}
		permissions, err := roles.ResolveExecutionPermissions(ctx, requester.UserID, requester.APIKeyID)
		if err != nil {
			return err
		}
		if !permissions.Allows(requester.Permission, requester.EnvironmentID) {
			return errors.New("requesting user no longer has permission for this backup")
		}
		if requester.Permission == authz.PermSystemBackupsManage && !permissions.IsGlobalAdmin() {
			return errors.New("system backups require a global administrator")
		}
		return nil
	})
	lc.Append(fx.Hook{OnStop: engine.Stop})
	return engine
}

func provideActorRuntimeInternal(
	appCtx context.Context,
	cfg *config.Config,
	db *database.DB,
	settingsService *settings.SettingsService,
	lc fx.Lifecycle,
	cancelApp context.CancelCauseFunc,
) (*francis.Runtime, error) {
	runtime, err := francis.New(cfg.DatabaseURL, "", "", cfg.ActorPort)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		sqlDB, sqlDBErr := db.SQLDB()
		if sqlDBErr != nil {
			return sqlDBErr
		}
		if migrateErr := francis.MigrateLegacyStore(ctx, sqlDB, cfg.DatabaseURL); migrateErr != nil {
			return migrateErr
		}
		if configureIdentityErr := runtime.ConfigureIdentity(cfg.EncryptionKey, settingsService.GetSettingsConfig().InstanceID.Value); configureIdentityErr != nil {
			return configureIdentityErr
		}
		return runtime.Start(ctx, appCtx, func(err error) {
			slog.ErrorContext(appCtx, "Francis host failed", "error", err)
			cancelApp(fmt.Errorf("francis host failed: %w", err))
		})
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

// provideWorkflowEngineInternal starts monitoring after the host is ready; its
// hook follows the runtime's because the engine depends on it.
func provideWorkflowEngineInternal(appCtx context.Context, runtime *francis.Runtime, coordinator *runs.Coordinator, activities *activity.ActivityService, lc fx.Lifecycle) (*flow.Engine, error) {
	engine, err := flow.New(appCtx, runtime, coordinator, activities)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStart: engine.Start, OnStop: engine.Stop})
	return engine, nil
}

func provideAdmissionGateInternal(runtime *francis.Runtime) (*runs.Admission, error) {
	admission := runs.NewAdmission(runtime.Service(), uuid.New().String())
	if err := admission.Register(runtime); err != nil {
		return nil, err
	}
	return admission, nil
}

func provideTunnelRegistryInternal(lc fx.Lifecycle) *edge.TunnelRegistry {
	localRegistry := edge.NewTunnelRegistry()
	edge.SetDefaultRegistry(localRegistry)
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		defer edge.ClearDefaultRegistry(localRegistry)
		return localRegistry.Stop(ctx)
	}})
	return localRegistry
}

func provideDockerClientServiceInternal(
	ctx context.Context,
	lc fx.Lifecycle,
	db *database.DB,
	cfg *config.Config,
	localSettings *settings.SettingsService,
	eventService *event.EventService,
) *docker.DockerClientService {
	service := docker.NewDockerClientService(ctx, db, cfg, localSettings, bus.WithDroppedEventCallback(func(message events.Message) {
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

func provideVersionServiceInternal(
	httpClient *http.Client,
	cfg *config.Config,
	localRegistry *registry.ContainerRegistryService,
	localDocker *docker.DockerClientService,
	imageUpdate *imageupdate.ImageUpdateService,
	settingsService *settings.SettingsService,
) *version.VersionService {
	return version.NewVersionService(httpClient, cfg.UpdateCheckDisabled, config.Version, config.Revision, localRegistry, localDocker, imageUpdate, settingsService)
}

func provideGitRepositoryServiceInternal(db *database.DB, cfg *config.Config, eventService *event.EventService, settingsService *settings.SettingsService) *gitrepo.GitRepositoryService {
	return gitrepo.NewGitRepositoryService(db, cfg.GitWorkDir, eventService, settingsService)
}

func provideS3ModuleInternal(service *s3domain.S3DestinationService, localEnvironment *environment.EnvironmentService) *s3domain.Module {
	return s3domain.New(service, localEnvironment.SyncS3DestinationsToRemoteEnvironments)
}

func provideS3ServiceInternal(db *database.DB, localEnvironment *environment.EnvironmentService) *s3domain.S3DestinationService {
	return s3domain.NewS3DestinationService(db, localEnvironment.CheckS3DestinationReferences)
}

func provideAuthModuleInternal(service *auth.AuthService, userService *user.UserService, settingsService *settings.SettingsService, passkeyService *passkey.PasskeyService) *auth.Module {
	return auth.New(service, userService, settingsService, passkeyService.BeginMFAAuthentication)
}

func provideContainerRegistryServiceInternal(
	db *database.DB,
	localDocker *docker.DockerClientService,
	kvService *kv.KVService,
	settingsService *settings.SettingsService,
) *registry.ContainerRegistryService {
	return registry.NewContainerRegistryService(db, func(ctx context.Context) (registry.RegistryDaemonClient, error) { return localDocker.GetClient(ctx) }, kvService, settingsService)
}

func provideContainerRegistryModuleInternal(service *registry.ContainerRegistryService, localEnvironment *environment.EnvironmentService) *registry.Module {
	return registry.New(service, localEnvironment.SyncRegistriesToRemoteEnvironments)
}

func provideProjectServiceInternal(
	db *database.DB,
	localSettings *settings.SettingsService,
	localEvent *event.EventService,
	localImage *image.ImageService,
	localDocker *docker.DockerClientService,
	localBuild *build.BuildService,
	lifecycleService *project.LifecycleService,
	localKv *kv.KVService,
	localRegistry *registry.ContainerRegistryService,
	localEnvironment *environment.EnvironmentService,
	cfg *config.Config,
) *project.ProjectService {
	return project.NewProjectService(db, localSettings, localEvent, localImage, localDocker, localBuild, lifecycleService, localRegistry, cfg, localKv, localEnvironment.GetEnabledRegistryCredentials)
}

// updaterServiceParams includes actor registration and worker lifecycle dependencies.
type updaterServiceParams struct {
	fx.In

	Config       *config.Config
	Workflows    *flow.Engine
	Coordinator  *runs.Coordinator
	Admission    *runs.Admission
	Roles        *role.RoleService
	DB           *database.DB
	Settings     *settings.SettingsService
	Docker       *docker.DockerClientService
	Project      *project.ProjectService
	ImageUpdate  *imageupdate.ImageUpdateService
	Registry     *registry.ContainerRegistryService
	Event        *event.EventService
	Image        *image.ImageService
	Notification *notification.NotificationService
	System       *system.SystemService
	Activity     *activity.ActivityService
}

// provideImageUpdateServiceInternal registers the image-check workflow while the host is still unstarted.
func provideImageUpdateServiceInternal(
	db *database.DB,
	settingsService *settings.SettingsService,
	registryService *registry.ContainerRegistryService,
	dockerService *docker.DockerClientService,
	eventService *event.EventService,
	notificationService *notification.NotificationService,
	activityService *activity.ActivityService,
	engine *flow.Engine,
) (*imageupdate.ImageUpdateService, error) {
	service := imageupdate.NewImageUpdateService(db, settingsService, registryService, dockerService, eventService, notificationService, activityService)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideImageServiceInternal registers the auto-patch workflow while the host is still unstarted.
func provideImageServiceInternal(
	db *database.DB,
	dockerService *docker.DockerClientService,
	registryService *registry.ContainerRegistryService,
	imageUpdateService *imageupdate.ImageUpdateService,
	vulnerabilityService *vulnerability.VulnerabilityService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	activityService *activity.ActivityService,
	engine *flow.Engine,
) (*image.ImageService, error) {
	service := image.NewImageService(db, dockerService, registryService, imageUpdateService, vulnerabilityService, eventService, settingsService, activityService)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideVulnerabilityServiceInternal registers the scheduled scan workflow while the host is still unstarted.
func provideVulnerabilityServiceInternal(
	db *database.DB,
	cfg *config.Config,
	dockerService *docker.DockerClientService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	notificationService *notification.NotificationService,
	activityService *activity.ActivityService,
	registryService *registry.ContainerRegistryService,
	kvService *kv.KVService,
	httpClient *http.Client,
	engine *flow.Engine,
) (*vulnerability.VulnerabilityService, error) {
	service := vulnerability.NewVulnerabilityService(db, cfg, dockerService, eventService, settingsService, notificationService, activityService, registryService, kvService, httpClient)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

func provideUpdaterServiceInternal(p updaterServiceParams) (*updater.UpdaterService, error) {
	service, err := updater.NewUpdaterService(
		p.DB,
		p.Settings,
		p.Docker,
		p.Project,
		p.ImageUpdate,
		p.Registry,
		p.Event,
		p.Image,
		p.Notification,
		p.System,
		p.Activity,
		p.Config,
		p.Coordinator,
		p.Admission,
		p.Roles,
	)
	if err != nil {
		return nil, err
	}
	if registerWorkflowsErr := service.RegisterWorkflows(p.Workflows); registerWorkflowsErr != nil {
		return nil, registerWorkflowsErr
	}
	return service, nil
}

func provideUserModuleInternal(service *user.UserService, localAuth *auth.AuthService, settingsService *settings.SettingsService) *user.Module {
	return user.New(service, localAuth.InvalidateUserTokenCache, settingsService)
}

func provideJWKSetManagerInternal(ctx context.Context, lc fx.Lifecycle) *oidcjwk.KeySetManager {
	manager := oidcjwk.NewKeySetManager(ctx)
	lc.Append(fx.Hook{OnStop: manager.Shutdown})
	return manager
}

func provideAuthMiddlewareInternal(
	authService *auth.AuthService,
	apiKey *apikey.ApiKeyService,
	env *environment.EnvironmentService,
	localRole *role.RoleService,
	cfg *config.Config,
) *auth.AuthMiddleware {
	return auth.NewAuthMiddleware(authService, cfg).
		WithApiKeyValidator(apiKey).
		WithEnvironmentAccessTokenResolver(env).
		WithPermissionResolver(localRole)
}

func provideFilesystemWatcherJobInternal(
	ctx context.Context,
	lc fx.Lifecycle,
	localProject *project.ProjectService,
	localTemplate *template.TemplateService,
	localSettings *settings.SettingsService,
	cfg *config.Config,
) (
	*scheduler.FilesystemWatcherJob,
	error,
) {
	job, err := scheduler.NewFilesystemWatcherJob(ctx, localProject, localTemplate, localSettings, cfg.ProjectScanMaxDepth)
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
			var stopWorkerErr error
			if stop != nil {
				stopWorkerErr = stop(stopCtx)
			}
			return errors.Join(stopWorkerErr, job.Stop(stopCtx))
		},
	})
	return job, nil
}

// provideGitOpsSyncServiceInternal registers the gitops-sync workflow while the host is still unstarted.
func provideGitOpsSyncServiceInternal(
	db *database.DB,
	repoService *gitrepo.GitRepositoryService,
	projectService *project.ProjectService,
	swarmService *swarm.SwarmService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	engine *flow.Engine,
) (*gitops.GitOpsSyncService, error) {
	service := gitops.NewGitOpsSyncService(db, repoService, projectService, swarmService, eventService, settingsService)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideVolumeServiceInternal registers the volume backup workflows while the host is still unstarted.
func provideVolumeServiceInternal(
	lc fx.Lifecycle,
	db *database.DB,
	dockerService *docker.DockerClientService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	containerService *container.ContainerService,
	imageService *image.ImageService,
	backupEngine *backup.Engine,
	s3Destinations *s3domain.S3DestinationService,
	cfg *config.Config,
	recoveryKeys *backup.RecoveryKeyStore,
	engine *flow.Engine,
) (*volume.VolumeService, error) {
	service := volume.NewVolumeService(db, dockerService, eventService, settingsService, containerService, imageService, backupEngine, s3Destinations, cfg, recoveryKeys)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error { service.CleanupHelperContainers(ctx); return nil }})
	return service, nil
}

// provideSystemServiceInternal registers the system backup workflows while the host is still unstarted.
func provideSystemServiceInternal(
	db *database.DB,
	dockerService *docker.DockerClientService,
	containerService *container.ContainerService,
	imageUpdateService *imageupdate.ImageUpdateService,
	volumeService *volume.VolumeService,
	networkService *network.NetworkService,
	settingsService *settings.SettingsService,
	activityService *activity.ActivityService,
	versionService *version.VersionService,
	eventService *event.EventService,
	projectService *project.ProjectService,
	backupEngine *backup.Engine,
	s3Destinations *s3domain.S3DestinationService,
	recoveryKeys *backup.RecoveryKeyStore,
	actors *francis.Runtime,
	cfg *config.Config,
	environments *environment.EnvironmentService,
	engine *flow.Engine,
	roles *role.RoleService,
) (*system.SystemService, error) {
	service := system.NewSystemService(
		db, dockerService, containerService, imageUpdateService, volumeService, networkService, settingsService, activityService,
		versionService, eventService, projectService, backupEngine, s3Destinations, recoveryKeys, actors, cfg,
	)
	if err := service.RegisterWorkflows(engine, environments, roles); err != nil {
		return nil, err
	}
	return service, nil
}
