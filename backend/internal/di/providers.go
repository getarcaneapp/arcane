package di

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/network"
	"github.com/getarcaneapp/arcane/backend/v3/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/internal/template"
	"github.com/getarcaneapp/arcane/backend/v3/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v3/internal/version"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/oidcjwk"
)

func provideBackupEngine(lc fx.Lifecycle, admission *runs.Admission, imageService *image.ImageService, roles *role.RoleService, cfg *config.Config) *backup.Engine {
	engine := backup.NewEngine(admission, imageService)
	engine.SetAuthorize(func(ctx context.Context, requester types.BackupRequester) error {
		if cfg.AgentEnabled && requester.UserID == "agent" && requester.APIKeyID == "" {
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

func provideActorRuntime(
	appCtx context.Context,
	cfg *config.Config,
	settingsService *settings.SettingsService,
	lc fx.Lifecycle,
	cancelApp context.CancelCauseFunc,
	encryptionKey string,
) (*francis.Runtime, error) {
	runtime, err := francis.New(cfg.DatabaseURL, "", "", cfg.ActorPort)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if configureIdentityErr := runtime.ConfigureIdentity(encryptionKey, settingsService.GetSettingsConfig().InstanceID.Value); configureIdentityErr != nil {
			return configureIdentityErr
		}
		return runtime.Start(ctx, appCtx, func(err error) {
			slog.ErrorContext(appCtx, "Francis host failed", "error", err)
			cancelApp(fmt.Errorf("francis host failed: %w", err))
		})
	}, OnStop: runtime.Stop})
	return runtime, nil
}

func provideRunCoordinator(runtime *francis.Runtime, store *kv.KVService, cfg *config.Config) (*runs.Coordinator, error) {
	coordinator := runs.New(store, runtime.Service(), cfg.GetLocation())
	if err := coordinator.Register(runtime); err != nil {
		return nil, err
	}
	return coordinator, nil
}

// provideWorkflowEngine starts monitoring after the host is ready; its
// hook follows the runtime's because the engine depends on it.
func provideWorkflowEngine(appCtx context.Context, runtime *francis.Runtime, coordinator *runs.Coordinator, activities *activity.ActivityService, lc fx.Lifecycle) (*flow.Engine, error) {
	engine, err := flow.New(appCtx, runtime, coordinator, activities)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStart: engine.Start, OnStop: engine.Stop})
	return engine, nil
}

func provideAdmissionGate(runtime *francis.Runtime) (*runs.Admission, error) {
	admission := runs.NewAdmission(runtime.Service(), uuid.New().String())
	if err := admission.Register(runtime); err != nil {
		return nil, err
	}
	return admission, nil
}

func provideTunnelRegistry(lc fx.Lifecycle) *agent.TunnelRegistry {
	localRegistry := agent.NewTunnelRegistry()
	agent.SetDefaultRegistry(localRegistry)
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		defer agent.ClearDefaultRegistry(localRegistry)
		stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return localRegistry.Stop(stopCtx)
	}})
	return localRegistry
}

func provideDockerClientService(
	ctx context.Context,
	lc fx.Lifecycle,
	db *database.DB,
	localSettings *settings.SettingsService,
	eventService *event.EventService,
) *docker.DockerClientService {
	service := docker.NewDockerClientService(ctx, db, localSettings, bus.WithDroppedEventCallback(func(message events.Message) {
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

// updaterServiceParams includes actor registration and worker lifecycle dependencies.
type updaterServiceParams struct {
	fx.In

	Config       *config.Config
	Workflows    *flow.Engine
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

// provideImageUpdateService registers the image-check workflow while the host is still unstarted.
func provideImageUpdateService(
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

// provideImageService registers the auto-patch workflow while the host is still unstarted.
func provideImageService(
	db *database.DB,
	dockerService *docker.DockerClientService,
	registryService *registry.ContainerRegistryService,
	imageUpdateService *imageupdate.ImageUpdateService,
	vulnerabilityService *vulnerability.VulnerabilityService,
	eventService *event.EventService,
	settingsService *settings.SettingsService,
	activityService *activity.ActivityService,
	engine *flow.Engine,
	cfg *config.Config,
) (*image.ImageService, error) {
	service := image.NewImageService(db, dockerService, registryService, imageUpdateService, vulnerabilityService, eventService, settingsService, activityService, cfg)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideVulnerabilityService registers the scheduled scan workflow while the host is still unstarted.
func provideVulnerabilityService(
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

func provideUpdaterService(p updaterServiceParams) (*updater.UpdaterService, error) {
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

func provideJWKSetManager(ctx context.Context, lc fx.Lifecycle) *oidcjwk.KeySetManager {
	manager := oidcjwk.NewKeySetManager(ctx)
	lc.Append(fx.Hook{OnStop: manager.Shutdown})
	return manager
}

func provideAuthMiddleware(
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

func provideFilesystemWatcherJob(
	ctx context.Context,
	lc fx.Lifecycle,
	localProject *project.ProjectService,
	localTemplate *template.TemplateService,
	localSettings *settings.SettingsService,
) (
	*scheduler.FilesystemWatcherJob,
	error,
) {
	job, err := scheduler.NewFilesystemWatcherJob(ctx, localProject, localTemplate, localSettings)
	if err != nil {
		return nil, err
	}
	var stop func(context.Context) error
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			stop, err = concurrency.StartSupervised(ctx, "filesystem watcher", job.Supervise)
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

// provideGitOpsSyncService registers the gitops-sync workflow while the host is still unstarted.
func provideGitOpsSyncService(
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

// provideVolumeService registers the volume backup workflows while the host is still unstarted.
func provideVolumeService(
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
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		service.CleanupHelperContainers(cleanupCtx)
		return nil
	}})
	return service, nil
}

// provideSystemService registers the system backup workflows while the host is still unstarted.
func provideSystemService(
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
	registryService *registry.ContainerRegistryService,
	backupEngine *backup.Engine,
	s3Destinations *s3domain.S3DestinationService,
	recoveryKeys *backup.RecoveryKeyStore,
	actors *francis.Runtime,
	cfg *config.Config,
	settingsEnv settings.Env,
	environments *environment.EnvironmentService,
	engine *flow.Engine,
	roles *role.RoleService,
) (*system.SystemService, error) {
	service := system.NewSystemService(
		db, dockerService, containerService, imageUpdateService, volumeService, networkService, settingsService, activityService,
		versionService, eventService, projectService, registryService, backupEngine, s3Destinations, recoveryKeys, actors, cfg,
		settingsEnv,
	)
	if err := service.RegisterWorkflows(engine, environments, roles); err != nil {
		return nil, err
	}
	return service, nil
}
