package di

import (
	"context"
	"embed"
	"log/slog"
	"net/http"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/federated"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
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
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"

	"emperror.dev/errors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"go.uber.org/fx"
)

func provideResourcesFSInternal() embed.FS {
	return resources.FS
}

func provideEventModuleInternal(db *database.DB, cfg *config.Config, httpClient *http.Client) *event.Module {
	return event.New(event.Dependencies{DB: db, Config: cfg, HTTPClient: httpClient})
}

func provideEventServiceInternal(module *event.Module) *event.EventService {
	return module.Service()
}

func provideActivityModuleInternal(db *database.DB, settingsService *settings.SettingsService, environment *environment.EnvironmentService) *activity.Module {
	return activity.New(activity.Dependencies{
		DB:       db,
		Settings: settingsService,
		Environment: activity.EnvironmentDependencies{
			ProxyJSONRequest:               environment.ProxyJSONRequest,
			ListRemoteEnvironments:         environment.ListRemoteEnvironments,
			GetActiveRemoteEnvironment:     environment.GetActiveRemoteEnvironmentSnapshot,
			ProxyJSONRequestForEnvironment: environment.ProxyJSONRequestForEnvironment,
			ResolveEnvironmentName:         environment.ResolveEnvironmentName,
		},
	})
}

func provideActivityServiceInternal(module *activity.Module) *activity.ActivityService {
	return module.Service()
}

func provideEnvironmentModuleInternal(service *environment.EnvironmentService, settingsService *settings.SettingsService, apiKey *apikey.ApiKeyService, eventService *event.EventService, cfg *config.Config) *environment.Module {
	return environment.New(service, environment.Dependencies{
		Settings: settingsService,
		ApiKey:   apiKey,
		Event:    eventService,
		Config:   cfg,
	})
}

func provideSwarmModuleInternal(service *swarm.SwarmService, environmentService *environment.EnvironmentService, eventService *event.EventService, cfg *config.Config) *swarm.Module {
	return swarm.New(service, swarm.Dependencies{
		Environment: environmentService,
		Event:       eventService,
		Config:      cfg,
	})
}

func provideImageUpdateModuleInternal(service *imageupdate.ImageUpdateService, imageService *image.ImageService) *imageupdate.Module {
	return imageupdate.New(service, imageupdate.Dependencies{
		GetUpdateInfoByImageRefs: imageService.GetUpdateInfoByImageRefs,
	})
}

func provideImageModuleInternal(service *image.ImageService, dockerService *docker.DockerClientService, imageUpdate *imageupdate.ImageUpdateService, settingsService *settings.SettingsService, buildService *build.BuildService, activityService *activity.ActivityService, uploadService *upload.UploadService) *image.Module {
	return image.New(service, image.Dependencies{
		Docker:      dockerService,
		ImageUpdate: imageUpdate,
		Settings:    settingsService,
		Build:       buildService,
		Activity:    activityService,
		Upload:      uploadService,
	})
}

func provideRoleModuleInternal(db *database.DB) *role.Module {
	return role.New(role.Dependencies{DB: db})
}

func provideRoleServiceInternal(module *role.Module) *role.RoleService {
	return module.Service()
}

func provideSettingsServiceInternal(ctx context.Context, lc fx.Lifecycle, db *database.DB, runtime *actors.Runtime) (*settings.SettingsService, error) {
	executor, err := actors.NewExecutor(ctx, runtime, "services", "settings", 3)
	if err != nil {
		return nil, err
	}
	effects, err := actors.NewExecutor(ctx, runtime, "services", "settings-effects", 3)
	if err != nil {
		return nil, errors.Combine(err, executor.Stop(ctx))
	}
	service, err := settings.NewSettingsService(ctx, db, executor, effects)
	if err != nil {
		return nil, errors.Combine(err, executor.Stop(ctx), effects.Stop(ctx))
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			return errors.Combine(executor.Stop(ctx), effects.Stop(ctx))
		},
	})
	return service, nil
}

func provideSettingsModuleInternal(service *settings.SettingsService, environment *environment.EnvironmentService, cfg *config.Config) *settings.Module {
	return settings.New(service, settings.Dependencies{
		Search:          settings.NewSettingsSearchService(),
		ProxyRemoteJSON: environment.ProxyJSONRequest,
		Config:          cfg,
	})
}

func provideBackupEngineInternal(ctx context.Context, lc fx.Lifecycle, runtime *actors.Runtime, admission *actors.Gate[actors.AdmissionKey], imageService *image.ImageService) *backup.Engine {
	engine := backup.NewEngine(ctx, runtime, admission, imageService)
	lc.Append(fx.Hook{OnStop: engine.Stop})
	return engine
}

func provideAdmissionGateInternal(ctx context.Context, lc fx.Lifecycle, runtime *actors.Runtime) (*actors.Gate[actors.AdmissionKey], error) {
	gate, err := actors.NewGate[actors.AdmissionKey](ctx, runtime, "admission", "application")
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: gate.Stop})
	return gate, nil
}

func provideTunnelRegistryInternal(ctx context.Context, lc fx.Lifecycle, runtime *actors.Runtime) (*edge.TunnelRegistry, error) {
	registry, err := edge.NewActorTunnelRegistry(ctx, runtime)
	if err != nil {
		return nil, err
	}
	edge.SetDefaultRegistry(registry)
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			defer edge.ClearDefaultRegistry(registry)
			return registry.Stop(ctx)
		},
	})
	return registry, nil
}

func provideDockerClientServiceInternal(ctx context.Context, lc fx.Lifecycle, actorRuntime *actors.Runtime, db *database.DB, cfg *config.Config, settings *settings.SettingsService) *docker.DockerClientService {
	service := docker.NewDockerClientService(ctx, db, cfg, settings)
	var runner *actors.Runner
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var err error
			runner, err = actors.NewRunner(ctx, actorRuntime, "docker-events", "stream", "Docker event watcher", 3, func(runCtx context.Context) error {
				service.WatchEvents(runCtx)
				return nil
			})
			return err
		},
		OnStop: func(stopCtx context.Context) error {
			err := runner.Stop(stopCtx)
			service.Close()
			return err
		},
	})
	return service
}

func provideVersionServiceInternal(httpClient *http.Client, cfg *config.Config, registry *registry.ContainerRegistryService, docker *docker.DockerClientService, imageUpdate *imageupdate.ImageUpdateService) *version.VersionService {
	return version.NewVersionService(httpClient, cfg.UpdateCheckDisabled, config.Version, config.Revision, registry, docker, imageUpdate)
}

func provideGitRepositoryModuleInternal(db *database.DB, cfg *config.Config, eventService *event.EventService, settingsService *settings.SettingsService) *gitrepo.Module {
	return gitrepo.New(gitrepo.Dependencies{DB: db, WorkDir: cfg.GitWorkDir, Event: eventService, Settings: settingsService})
}

func provideGitRepositoryServiceInternal(module *gitrepo.Module) *gitrepo.GitRepositoryService {
	return module.Service()
}

func provideS3ModuleInternal(db *database.DB, environmentService *environment.EnvironmentService) *s3domain.Module {
	return s3domain.New(s3domain.Dependencies{
		DB:                     db,
		SyncRemoteDestinations: environmentService.SyncS3DestinationsToRemoteEnvironments,
		CheckRemoteReferences:  environmentService.CheckS3DestinationReferences,
	})
}

func provideS3ServiceInternal(module *s3domain.Module) *s3domain.S3DestinationService {
	return module.Service()
}

func provideVolumeModuleInternal(lc fx.Lifecycle, db *database.DB, dockerService *docker.DockerClientService, eventService *event.EventService, settingsService *settings.SettingsService, imageService *image.ImageService, activityService *activity.ActivityService, containerModule *container.Module, engine *backup.Engine, s3Service *s3domain.S3DestinationService, environmentService *environment.EnvironmentService, cfg *config.Config, uploadService *upload.UploadService) *volume.Module {
	module := volume.New(volume.Dependencies{
		DB:          db,
		Docker:      dockerService,
		Event:       eventService,
		Settings:    settingsService,
		Image:       imageService,
		Activity:    activityService,
		Environment: environmentService,
		Container:   containerModule.Service(),
		Engine:      engine,
		S3:          s3Service,
		Config:      cfg,
		Upload:      uploadService,
	})
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			module.Service().CleanupHelperContainers(ctx)
			return nil
		},
	})
	return module
}

func provideVolumeServiceInternal(module *volume.Module) *volume.VolumeService {
	return module.Service()
}

func provideSystemBackupServiceInternal(db *database.DB, dockerService *docker.DockerClientService, volumeModule *volume.Module, engine *backup.Engine, s3Service *s3domain.S3DestinationService, activityService *activity.ActivityService, settingsService *settings.SettingsService, cfg *config.Config) *systembackup.SystemBackupService {
	return systembackup.NewSystemBackupService(db, dockerService, volumeModule.Service(), engine, s3Service, activityService, settingsService, cfg)
}

func provideContainerModuleInternal(event *event.EventService, docker *docker.DockerClientService, image *image.ImageService, settings *settings.SettingsService, project *project.ProjectService, activity *activity.ActivityService) *container.Module {
	return container.New(container.Dependencies{
		Event:    event,
		Docker:   docker,
		Image:    image,
		Settings: settings,
		Project:  project,
		Activity: activity,
	})
}

func provideAuthModuleInternal(service *auth.AuthService, userService *user.UserService, settingsService *settings.SettingsService, passkeyService *passkey.PasskeyService) *auth.Module {
	return auth.New(auth.Dependencies{
		Service:                service,
		User:                   userService,
		Settings:               settingsService,
		BeginMFAAuthentication: passkeyService.BeginMFAAuthentication,
	})
}

func provideContainerRegistryModuleInternal(db *database.DB, dockerService *docker.DockerClientService, kvService *kv.KVService, environment *environment.EnvironmentService) *registry.Module {
	return registry.New(registry.Dependencies{DB: db, Docker: dockerService, KV: kvService, SyncRemoteRegistries: environment.SyncRegistriesToRemoteEnvironments})
}

func provideContainerRegistryServiceInternal(module *registry.Module) *registry.ContainerRegistryService {
	return module.Service()
}

func provideProjectServiceInternal(db *database.DB, settings *settings.SettingsService, event *event.EventService, image *image.ImageService, docker *docker.DockerClientService, build *build.BuildService, lifecycleService *project.LifecycleService, kv *kv.KVService, registry *registry.ContainerRegistryService, environment *environment.EnvironmentService, cfg *config.Config) *project.ProjectService {
	return project.NewProjectService(db, settings, event, image, docker, build, lifecycleService, registry, cfg).
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

func provideUpdaterModuleInternal(p updaterServiceParams) (*updater.Module, error) {
	return updater.New(updater.Dependencies{
		DB:           p.DB,
		Settings:     p.Settings,
		Docker:       p.Docker,
		Project:      p.Project,
		ImageUpdate:  p.ImageUpdate,
		Registry:     p.Registry,
		Event:        p.Event,
		Image:        p.Image,
		Notification: p.Notification,
		SelfUpgrade:  p.SystemUpgrade,
		Activity:     p.Activity,
	})
}

func provideUserServiceInternal(db *database.DB, role *role.RoleService) *user.UserService {
	return user.NewUserService(db).WithRoleService(role)
}

func provideUserModuleInternal(service *user.UserService, auth *auth.AuthService, settingsService *settings.SettingsService) *user.Module {
	return user.New(user.Dependencies{Service: service, InvalidateUserTokenCache: auth.InvalidateUserTokenCache, Settings: settingsService})
}

func provideJobModuleInternal(db *database.DB, settingsService *settings.SettingsService, cfg *config.Config, environment *environment.EnvironmentService) *job.Module {
	return job.New(job.Dependencies{DB: db, Settings: settingsService, Config: cfg, Environment: environment})
}

func provideJobServiceInternal(module *job.Module) *job.JobService {
	return module.Service()
}

func provideTemplateModuleInternal(ctx context.Context, db *database.DB, httpClient *http.Client, settingsService *settings.SettingsService) *template.Module {
	return template.New(template.Dependencies{Context: ctx, DB: db, HTTPClient: httpClient, Settings: settingsService})
}

func provideTemplateServiceInternal(module *template.Module) *template.TemplateService {
	return module.Service()
}

func provideApiKeyModuleInternal(db *database.DB, userService *user.UserService, roleService *role.RoleService) *apikey.Module {
	return apikey.New(apikey.Dependencies{DB: db, User: userService, Role: roleService})
}

func provideApiKeyServiceInternal(module *apikey.Module) *apikey.ApiKeyService {
	return module.Service()
}

func provideFederatedCredentialServiceInternal(db *database.DB, auth *auth.AuthService, user *user.UserService, settings *settings.SettingsService, event *event.EventService, httpClient *http.Client, role *role.RoleService) *federated.FederatedCredentialService {
	return federated.NewFederatedCredentialService(db, auth, user, settings, event, httpClient).WithRoleService(role)
}

func provideAuthMiddlewareInternal(authService *auth.AuthService, apiKey *apikey.ApiKeyService, env *environment.EnvironmentService, role *role.RoleService, cfg *config.Config) *auth.AuthMiddleware {
	return auth.NewAuthMiddleware(authService, cfg).
		WithApiKeyValidator(apiKey).
		WithEnvironmentAccessTokenResolver(env).
		WithPermissionResolver(role)
}

func provideFilesystemWatcherJobInternal(ctx context.Context, lc fx.Lifecycle, actorRuntime *actors.Runtime, project *project.ProjectService, template *template.TemplateService, settings *settings.SettingsService, cfg *config.Config) (*scheduler.FilesystemWatcherJob, error) {
	job, err := scheduler.NewFilesystemWatcherJob(ctx, actorRuntime, project, template, settings, cfg.ProjectScanMaxDepth)
	if err != nil {
		return nil, err
	}
	var runner *actors.Runner
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			runner, err = actors.NewRunner(ctx, actorRuntime, "filesystem-watcher", "lifecycle", "filesystem watcher", 3, func(runCtx context.Context) error {
				if startErr := job.Start(runCtx); startErr != nil {
					return startErr
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
			return errors.Combine(runner.Stop(stopCtx), job.Stop(stopCtx))
		},
	})
	return job, nil
}

func provideDashboardModuleInternal(db *database.DB, docker *docker.DockerClientService, containerModule *container.Module, project *project.ProjectService, image *image.ImageService, settings *settings.SettingsService, vulnerability *vulnerability.VulnerabilityService, environment *environment.EnvironmentService, version *version.VersionService, volumeModule *volume.Module) *dashboard.Module {
	return dashboard.New(dashboard.Dependencies{
		DB:            db,
		Docker:        docker,
		Container:     containerModule.Service(),
		Project:       project,
		Image:         image,
		Settings:      settings,
		Vulnerability: vulnerability,
		Environment:   environment,
		Version:       version,
		Volume:        volumeModule.Service(),
	})
}

func provideSystemModuleInternal(db *database.DB, cfg *config.Config, docker *docker.DockerClientService, containerModule *container.Module, image *image.ImageService, volumeModule *volume.Module, network *network.NetworkService, settings *settings.SettingsService, activity *activity.ActivityService, upgrade *system.SystemUpgradeService, environment *environment.EnvironmentService) *system.Module {
	return system.New(system.Dependencies{
		DB:            db,
		Config:        cfg,
		Docker:        docker,
		Container:     containerModule.Service(),
		Image:         image,
		Volume:        volumeModule.Service(),
		Network:       network,
		Settings:      settings,
		Activity:      activity,
		SystemUpgrade: upgrade,
		Environment:   environment,
	})
}

func provideWebhookModuleInternal(lc fx.Lifecycle, db *database.DB, containerModule *container.Module, updaterModule *updater.Module, project *project.ProjectService, gitOpsSync *gitops.GitOpsSyncService, event *event.EventService, environment *environment.EnvironmentService) *webhook.Module {
	module := webhook.New(webhook.Dependencies{
		DB:          db,
		Container:   containerModule.Service(),
		Updater:     updaterModule.Service(),
		Project:     project,
		GitOpsSync:  gitOpsSync,
		Event:       event,
		Environment: environment,
	})
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			// Drain fails only when the stop context expires; fx then skips all
			// remaining teardown regardless of what is returned here, so an
			// error would only mark an expected long action as a failed stop.
			if err := module.Service().DrainActions(ctx); err != nil {
				slog.WarnContext(ctx, "shutdown proceeding with webhook actions still in flight", "error", err)
			}
			return nil
		},
	})
	return module
}
