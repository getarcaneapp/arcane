// Package di owns the backend's dependency-injection graph.
package di

import (
	"context"
	"log/slog"

	"emperror.dev/emperror"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/federated"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imagepatch"
	"github.com/getarcaneapp/arcane/backend/v2/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/internal/passkey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/port"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	activitylib "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/activity"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/logging"
	"github.com/getarcaneapp/arcane/backend/v2/resources"
	"go.uber.org/fx"
)

// ActorOptions provides the durable Francis runtime and admission.
var ActorOptions = fx.Options(
	fx.Provide(provideActorRuntimeInternal, provideRunCoordinatorInternal),
	fx.Provide(provideAdmissionGateInternal),
	fx.Provide(provideTunnelRegistryInternal),
)

// ServiceOptions constructs services once and injects them into route modules.
var ServiceOptions = fx.Options(
	fx.Supply(
		fx.Annotate(new(logging.SlogErrorHandler), fx.As(new(emperror.ErrorHandler))),
		resources.FS,
	),
	fx.Provide(
		fx.Annotate(activity.NewActivityService, fx.As(fx.Self()), fx.As(new(activitylib.Service))),
		event.NewEventService,
		job.NewJobService,
		role.NewRoleService,
		apikey.NewApiKeyService,
		user.NewUserService,
		federated.NewFederatedCredentialService,
		kv.NewKVService,
		appimages.NewApplicationImagesService,
		session.NewSessionService,
		passkey.NewPasskeyService,
		environment.NewEnvironmentService,
		apns.NewApnsService,
		notification.NewNotificationService,
		vulnerability.NewVulnerabilityService,
		imagepatch.NewImagePatchService,
		imageupdate.NewImageUpdateService,
		image.NewImageService,
		build.NewBuildService,
		build.NewBuildWorkspaceService,
		project.NewLifecycleService,
		container.NewContainerService,
		dashboard.NewDashboardService,
		network.NewNetworkService,
		port.NewPortService,
		swarm.NewSwarmService,
		template.NewTemplateService,
		oidc.NewOidcService,
		system.NewSystemService,
		system.NewSystemUpgradeService,
		diagnostics.NewDiagnosticsService,
		gitops.NewGitOpsSyncService,
		variable.NewVariableService,
		backup.NewRecoveryKeyStore,
		upload.NewUploadService,
		systembackup.NewSystemBackupService,
		auth.NewAuthService,
		settings.NewSettingsSearchService,
		fx.Annotate(settings.NewSettingsService, fx.OnStop(func(ctx context.Context, service *settings.SettingsService) error { return service.Stop(ctx) })),
		fx.Annotate(volume.NewVolumeService, fx.OnStop(func(ctx context.Context, service *volume.VolumeService) { service.CleanupHelperContainers(ctx) })),
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
		provideJWKSetManagerInternal,
		provideDockerClientServiceInternal,
		provideProjectServiceInternal,
		provideBackupEngineInternal,
		provideVersionServiceInternal,
		provideGitRepositoryServiceInternal,
		provideS3ServiceInternal,
		provideContainerRegistryServiceInternal,
		provideUpdaterServiceInternal,
		provideAuthMiddlewareInternal,
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
		imagepatch.New,
		project.New,
		gitops.New,
		variable.New,
		upload.New,
		systembackup.New,
		search.New,
		provideActivityModuleInternal,
		provideImageUpdateModuleInternal,
		provideSettingsModuleInternal,
		provideS3ModuleInternal,
		provideContainerRegistryModuleInternal,
		provideAuthModuleInternal,
		provideUserModuleInternal,
	),
)

// JobOptions provides every scheduler job. Registration and settings callbacks
// remain bootstrap concerns because their ordering is application-specific.
var JobOptions = fx.Options(
	fx.Provide(
		scheduler.NewAutoUpdateJob,
		scheduler.NewImageUpdateWatcher,
		scheduler.NewDockerClientRefreshJob,
		scheduler.NewAnalyticsJob,
		scheduler.NewEventCleanupJob,
		scheduler.NewPruningVolumeHelperJob,
		scheduler.NewExpiredSessionsCleanupJob,
		scheduler.NewScheduledPruneJob,
		provideFilesystemWatcherJobInternal,
		scheduler.NewVulnerabilityScanJob,
		scheduler.NewVulnerabilityRiskJob,
		scheduler.NewAutoPatchJob,
		scheduler.NewAutoHealJob,
		scheduler.NewActivitySweepJob,
		scheduler.NewUploadSessionsCleanupJob,
		scheduler.NewGitCloneCleanupJob,
		fx.Annotate(scheduler.NewUpgradeLogCleanupJob, fx.ResultTags(`name:"upgrade-log-cleanup"`)),
		scheduler.NewApnsOutboxJob,
	),
)
