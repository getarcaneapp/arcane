// Package di owns the backend's dependency-injection graph.
package di

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"go.uber.org/fx"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/apikey"
	"getarcane.dev/arcane/v3/internal/apns"
	"getarcane.dev/arcane/v3/internal/appimages"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/build"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/dashboard"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/diagnostics"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/environment"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/federated"
	"getarcane.dev/arcane/v3/internal/gitops"
	"getarcane.dev/arcane/v3/internal/gitrepo"
	"getarcane.dev/arcane/v3/internal/health"
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/internal/imageupdate"
	"getarcane.dev/arcane/v3/internal/job"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/network"
	"getarcane.dev/arcane/v3/internal/notification"
	"getarcane.dev/arcane/v3/internal/oidc"
	"getarcane.dev/arcane/v3/internal/passkey"
	"getarcane.dev/arcane/v3/internal/podman"
	"getarcane.dev/arcane/v3/internal/port"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/role"
	s3domain "getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler"
	"getarcane.dev/arcane/v3/internal/search"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/internal/updater"
	"getarcane.dev/arcane/v3/internal/upload"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/variable"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/arcane/v3/internal/webhook"
	activitylib "getarcane.dev/arcane/v3/pkg/activity"
	libpodman "getarcane.dev/arcane/v3/pkg/podman"
	"getarcane.dev/arcane/v3/resources"
)

var (
	// ActorOptions provides the durable Francis runtime and admission.
	ActorOptions = fx.Options(
		fx.Provide(provideActorRuntime, provideRunCoordinator),
		fx.Provide(provideAdmissionGate),
		fx.Provide(provideWorkflowEngine),
		fx.Provide(provideTunnelRegistry),
	)

	// ServiceOptions constructs services once and injects them into route modules.
	ServiceOptions = fx.Options(
		fx.Supply(
			resources.FS,
		),
		fx.Provide(
			fx.Annotate(activity.NewActivityService, fx.As(fx.Self()), fx.As(new(activitylib.Service))),
			fx.Annotate(event.NewEventService, fx.OnStop(func(ctx context.Context, service *event.EventService) error { return service.Stop(ctx) })),
			job.NewJobService,
			fx.Annotate(role.NewRoleService, fx.OnStop((*role.RoleService).Stop)),
			fx.Annotate(apikey.NewApiKeyService, fx.OnStop((*apikey.ApiKeyService).Stop)),
			func(db *database.DB, roles *role.RoleService) *user.UserService {
				return user.NewUserService(db, roles, session.RevokeAllUserSessionsExceptInDB)
			},
			federated.NewFederatedCredentialService,
			kv.NewKVService,
			appimages.NewApplicationImagesService,
			session.NewSessionService,
			passkey.NewPasskeyService,
			fx.Annotate(environment.NewEnvironmentService,
				fx.OnStart(func(ctx context.Context, service *environment.EnvironmentService) error {
					return service.LoadAccessTokens(ctx)
				}),
			),
			apns.NewApnsService,
			notification.NewNotificationService,
			provideVulnerabilityService,
			provideImageUpdateService,
			provideImageService,
			build.NewBuildService,
			project.NewLifecycleService,
			container.NewContainerService,
			dashboard.NewDashboardService,
			network.NewNetworkService,
			port.NewPortService,
			swarm.NewSwarmService,
			template.NewTemplateService,
			oidc.NewOidcService,
			fx.Annotate(provideSystemService, fx.As(fx.Self()), fx.As(new(updater.SelfUpgrader))),
			diagnostics.NewDiagnosticsService,
			provideGitOpsSyncService,
			variable.NewVariableService,
			backup.NewRecoveryKeyStore,
			upload.NewUploadService,
			health.NewService,
			fx.Annotate(auth.NewAuthService, fx.OnStop((*auth.AuthService).Stop)),
			settings.NewSettingsSearchService,
			fx.Annotate(settings.NewSettingsService, fx.OnStop(func(ctx context.Context, service *settings.SettingsService) error { return service.Stop(ctx) })),
			provideVolumeService,
			fx.Annotate(webhook.NewWebhookService,
				fx.OnStart(func(ctx context.Context, service *webhook.WebhookService) error { return service.LoadTokenHashes(ctx) }),
				fx.OnStop(func(ctx context.Context, service *webhook.WebhookService) {
					if err := service.DrainActions(ctx); err != nil {
						slog.WarnContext(ctx, "shutdown proceeding with webhook actions still in flight", "error", err)
					}
				}),
			),
		),
		// These providers bind config values, callbacks, or managed resource lifetimes.
		fx.Provide(
			provideJWKSetManager,
			provideDockerClientService,
			func(
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
				return project.NewProjectService(project.ProjectServiceParams{
					DB:                          db,
					SettingsService:             localSettings,
					EventService:                localEvent,
					ImageService:                localImage,
					DockerService:               localDocker,
					BuildService:                localBuild,
					LifecycleService:            lifecycleService,
					ContainerRegistryService:    localRegistry,
					Config:                      cfg,
					KVService:                   localKv,
					RegistryCredentialsProvider: localEnvironment.GetEnabledRegistryCredentials,
				})
			},
			provideBackupEngine,
			func(
				httpClient *http.Client,
				cfg *config.Config,
				localRegistry *registry.ContainerRegistryService,
				localDocker *docker.DockerClientService,
				imageUpdate *imageupdate.ImageUpdateService,
				settingsService *settings.SettingsService,
			) *version.VersionService {
				return version.NewVersionService(version.VersionServiceParams{
					HTTPClient:               httpClient,
					AppVersion:               config.Version,
					Revision:                 config.Revision,
					ContainerRegistryService: localRegistry,
					DockerService:            localDocker,
					ImageUpdateService:       imageUpdate,
					SettingsService:          settingsService,
				})
			},
			func(db *database.DB, cfg *config.Config, eventService *event.EventService, settingsService *settings.SettingsService) *gitrepo.GitRepositoryService {
				return gitrepo.NewGitRepositoryService(db, cfg.GitWorkDir, eventService, settingsService)
			},
			func(db *database.DB, localEnvironment *environment.EnvironmentService) *s3domain.S3DestinationService {
				return s3domain.NewS3DestinationService(db, localEnvironment.CheckS3DestinationReferences)
			},
			func(db *database.DB, localDocker *docker.DockerClientService, kvService *kv.KVService, settingsService *settings.SettingsService) *registry.ContainerRegistryService {
				return registry.NewContainerRegistryService(registry.ContainerRegistryServiceParams{
					DB:              db,
					DockerClient:    func(ctx context.Context) (registry.RegistryDaemonClient, error) { return localDocker.GetClient(ctx) },
					KVService:       kvService,
					SettingsService: settingsService,
				})
			},
			func(localDocker *docker.DockerClientService, notificationService *notification.NotificationService) *podman.Service {
				return podman.NewService(podman.Config{
					Engine: localDocker.Engine,
					Host:   localDocker.DockerHost(),
					NewSystemd: func(rootless bool) libpodman.Systemd {
						return libpodman.NewSystemd(localDocker.DockerHost(), rootless, os.Getenv)
					},
					NotifyUpdates: notificationService.SendBatchContainerUpdateNotification,
				})
			},
			provideUpdaterService,
			provideAuthMiddleware,
		),
		fx.Provide(
			event.New,
			role.New,
			apikey.New,
			template.New,
			gitrepo.New,
			job.New,
			environment.New,
			image.New,
			container.New,
			swarm.New,
			podman.New,
			system.New,
			volume.New,
			updater.New,
			webhook.New,
			apns.New,
			notification.New,
			vulnerability.New,
			project.New,
			gitops.New,
			variable.New,
			upload.New,
			search.New,
			health.New,
			func(service *activity.ActivityService, localEnvironment *environment.EnvironmentService, eventService *event.EventService) *activity.Module {
				return activity.New(service, activity.EnvironmentDependencies{
					ProxyJSONRequest:               localEnvironment.ProxyJSONRequest,
					ListActiveRemoteEnvironments:   localEnvironment.ListActiveRemoteEnvironments,
					GetActiveRemoteEnvironment:     localEnvironment.GetActiveRemoteEnvironmentSnapshot,
					ProxyJSONRequestForEnvironment: localEnvironment.ProxyJSONRequestForEnvironment,
					ResolveEnvironmentName:         localEnvironment.ResolveEnvironmentName,
					Changes:                        eventService.Changes,
					EnvironmentChanges:             eventService.EnvironmentChanges,
				})
			},
			func(service *imageupdate.ImageUpdateService, imageService *image.ImageService) *imageupdate.Module {
				return imageupdate.New(service, imageService.GetUpdateInfoByImageRefs)
			},
			func(service *settings.SettingsService, search *settings.SettingsSearchService, localEnvironment *environment.EnvironmentService, cfg *config.Config) *settings.Module {
				return settings.New(service, search, localEnvironment.ProxyJSONRequest, cfg)
			},
			func(service *s3domain.S3DestinationService, localEnvironment *environment.EnvironmentService) *s3domain.Module {
				return s3domain.New(service, func(ctx context.Context) {
					localEnvironment.QueueRemoteSync(ctx, "S3 destinations", localEnvironment.SyncS3DestinationsToEnvironment)
				})
			},
			func(service *registry.ContainerRegistryService, localEnvironment *environment.EnvironmentService) *registry.Module {
				return registry.New(service, func(ctx context.Context) {
					localEnvironment.QueueRemoteSync(ctx, "registries", localEnvironment.SyncRegistriesToEnvironment)
				})
			},
			func(service *auth.AuthService, userService *user.UserService, settingsService *settings.SettingsService, passkeyService *passkey.PasskeyService) *auth.Module {
				return auth.New(service, userService, settingsService, passkeyService.BeginMFAAuthentication)
			},
			func(service *user.UserService, localAuth *auth.AuthService, settingsService *settings.SettingsService) *user.Module {
				return user.New(service, localAuth.InvalidateUserTokenCache, settingsService)
			},
		),
	)

	// JobOptions provides every scheduler job. Registration and settings callbacks
	// remain bootstrap concerns because their ordering is application-specific.
	JobOptions = fx.Options(
		fx.Provide(
			fx.Annotate(scheduler.NewAutoUpdateJob, fx.ResultTags(`name:"auto-update"`)),
			scheduler.NewImageUpdateWatcher,
			scheduler.NewDockerClientRefreshJob,
			scheduler.NewAnalyticsJob,
			scheduler.NewEventCleanupJob,
			scheduler.NewPruningVolumeHelperJob,
			scheduler.NewExpiredSessionsCleanupJob,
			scheduler.NewScheduledPruneJob,
			provideFilesystemWatcherJob,
			fx.Annotate(scheduler.NewVulnerabilityScanJob, fx.ResultTags(`name:"vulnerability-scan"`)),
			scheduler.NewVulnerabilityRiskJob,
			fx.Annotate(scheduler.NewAutoPatchJob, fx.ResultTags(`name:"auto-patch"`)),
			scheduler.NewAutoHealJob,
			scheduler.NewActivitySweepJob,
			scheduler.NewUploadSessionsCleanupJob,
			scheduler.NewGitCloneCleanupJob,
			scheduler.NewBackupRepositoryPruneJob,
			fx.Annotate(scheduler.NewUpgradeLogCleanupJob, fx.ResultTags(`name:"upgrade-log-cleanup"`)),
			scheduler.NewApnsOutboxJob,
		),
	)
)
