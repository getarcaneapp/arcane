// Package di owns the backend's dependency-injection graph.
package di

import (
	"context"
	"log/slog"
	"net/http"

	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/federated"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/health"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/port"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
)

// ActorOptions provides the durable Francis runtime and admission.
var ActorOptions = fx.Options(
	fx.Provide(provideActorRuntime, provideRunCoordinator),
	fx.Provide(provideAdmissionGate),
	fx.Provide(provideWorkflowEngine),
	fx.Provide(provideTunnelRegistry),
)

// ServiceOptions constructs services once and injects them into route modules.
var ServiceOptions = fx.Options(
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
		environment.NewEnvironmentService,
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
		provideSystemService,
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
			return project.NewProjectService(db, localSettings, localEvent, localImage, localDocker, localBuild, lifecycleService, localRegistry, cfg, localKv, localEnvironment.GetEnabledRegistryCredentials)
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
			return version.NewVersionService(httpClient, cfg.UpdateCheckDisabled, config.Version, config.Revision, localRegistry, localDocker, imageUpdate, settingsService)
		},
		func(db *database.DB, cfg *config.Config, eventService *event.EventService, settingsService *settings.SettingsService) *gitrepo.GitRepositoryService {
			return gitrepo.NewGitRepositoryService(db, cfg.GitWorkDir, eventService, settingsService)
		},
		func(db *database.DB, localEnvironment *environment.EnvironmentService) *s3domain.S3DestinationService {
			return s3domain.NewS3DestinationService(db, localEnvironment.CheckS3DestinationReferences)
		},
		func(db *database.DB, localDocker *docker.DockerClientService, kvService *kv.KVService, settingsService *settings.SettingsService) *registry.ContainerRegistryService {
			return registry.NewContainerRegistryService(db, func(ctx context.Context) (registry.RegistryDaemonClient, error) { return localDocker.GetClient(ctx) }, kvService, settingsService)
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
		dashboard.New,
		swarm.New,
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
		func(service *activity.ActivityService, localEnvironment *environment.EnvironmentService) *activity.Module {
			return activity.New(service, activity.EnvironmentDependencies{
				ProxyJSONRequest:               localEnvironment.ProxyJSONRequest,
				ListActiveRemoteEnvironments:   localEnvironment.ListActiveRemoteEnvironments,
				GetActiveRemoteEnvironment:     localEnvironment.GetActiveRemoteEnvironmentSnapshot,
				ProxyJSONRequestForEnvironment: localEnvironment.ProxyJSONRequestForEnvironment,
				ResolveEnvironmentName:         localEnvironment.ResolveEnvironmentName,
			})
		},
		func(service *imageupdate.ImageUpdateService, imageService *image.ImageService) *imageupdate.Module {
			return imageupdate.New(service, imageService.GetUpdateInfoByImageRefs)
		},
		func(service *settings.SettingsService, search *settings.SettingsSearchService, localEnvironment *environment.EnvironmentService, cfg *config.Config) *settings.Module {
			return settings.New(service, search, localEnvironment.ProxyJSONRequest, cfg)
		},
		func(service *s3domain.S3DestinationService, localEnvironment *environment.EnvironmentService) *s3domain.Module {
			return s3domain.New(service, localEnvironment.SyncS3DestinationsToRemoteEnvironments)
		},
		func(service *registry.ContainerRegistryService, localEnvironment *environment.EnvironmentService) *registry.Module {
			return registry.New(service, localEnvironment.SyncRegistriesToRemoteEnvironments)
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
var JobOptions = fx.Options(
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
