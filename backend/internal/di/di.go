// Package di owns the backend's dependency-injection graph.
package di

import (
	"emperror.dev/emperror"
	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apns"
	"github.com/getarcaneapp/arcane/backend/v2/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
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
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/upload"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/logging"
	"go.uber.org/fx"
)

// ActorOptions provides the shared in-process actor runtime.
var ActorOptions = fx.Options(
	fx.Provide(actors.NewRuntime),
	fx.Provide(provideAdmissionGateInternal),
	fx.Provide(provideTunnelRegistryInternal),
)

// ServiceOptions provides the backend service graph.
var ServiceOptions = fx.Options(
	fx.Supply(fx.Annotate(new(logging.SlogErrorHandler), fx.As(new(emperror.ErrorHandler)))),
	fx.Provide(
		// Infrastructure values consumed by services.
		provideResourcesFSInternal,
		provideJWKSetManagerInternal,

		// Services constructed directly through their public constructors.
		provideEventModuleInternal,
		(*event.Module).Service,
		provideActivityModuleInternal,
		(*activity.Module).Service,
		provideSettingsServiceInternal,
		kv.NewKVService,
		provideJobModuleInternal,
		(*job.Module).Service,
		search.New,
		appimages.NewApplicationImagesService,
		provideDockerClientServiceInternal,
		provideRoleModuleInternal,
		(*role.Module).Service,
		session.NewSessionService,
		passkey.NewPasskeyService,
		environment.NewEnvironmentService,
		provideEnvironmentModuleInternal,
		provideSettingsModuleInternal,
		apns.NewApnsService,
		apns.New,
		notification.NewNotificationService,
		notification.New,
		vulnerability.NewVulnerabilityService,
		vulnerability.New,
		imagepatch.NewImagePatchService,
		imagepatch.New,
		imageupdate.NewImageUpdateService,
		image.NewImageService,
		provideImageUpdateModuleInternal,
		provideImageModuleInternal,
		build.NewBuildService,
		build.NewBuildWorkspaceService,
		project.NewLifecycleService,
		provideProjectServiceInternal,
		project.New,
		provideContainerModuleInternal,
		provideDashboardModuleInternal,
		network.NewNetworkService,
		port.NewPortService,
		swarm.NewSwarmService,
		provideSwarmModuleInternal,
		provideTemplateModuleInternal,
		(*template.Module).Service,
		oidc.NewOidcService,
		provideSystemModuleInternal,
		system.NewSystemUpgradeService,
		diagnostics.NewDiagnosticsService,
		gitops.NewGitOpsSyncService,
		gitops.New,
		provideWebhookModuleInternal,
		variable.NewVariableService,
		variable.New,
		provideS3ModuleInternal,
		(*s3domain.Module).Service,
		provideBackupEngineInternal,
		backup.NewRecoveryKeyStore,
		upload.NewUploadService,
		upload.New,

		// Adapters for scalar config fields, unexported parameters, builders, and lifecycle hooks.
		provideVersionServiceInternal,
		provideGitRepositoryModuleInternal,
		(*gitrepo.Module).Service,
		provideVolumeModuleInternal,
		(*volume.Module).Service,
		systembackup.NewSystemBackupService,
		systembackup.New,
		auth.NewAuthService,
		provideAuthModuleInternal,
		provideContainerRegistryModuleInternal,
		(*registry.Module).Service,
		provideUpdaterModuleInternal,
		provideUserServiceInternal,
		provideUserModuleInternal,
		provideApiKeyModuleInternal,
		(*apikey.Module).Service,
		provideFederatedCredentialServiceInternal,
		provideAuthMiddlewareInternal,
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
