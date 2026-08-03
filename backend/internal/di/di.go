// Package di owns the backend's dependency-injection graph.
package di

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	arcanelogging "github.com/getarcaneapp/arcane/backend/v2/internal/logging"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
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
	fx.Provide(
		// Infrastructure values consumed by services.
		provideResourcesFSInternal,
		arcanelogging.NewSlogErrorHandler,

		// Services constructed directly through their public constructors.
		services.NewEventService,
		services.NewActivityService,
		provideSettingsServiceInternal,
		services.NewKVService,
		services.NewJobService,
		services.NewSettingsSearchService,
		services.NewCustomizeSearchService,
		services.NewApplicationImagesService,
		provideDockerClientServiceInternal,
		services.NewRoleService,
		services.NewSessionService,
		services.NewPasskeyService,
		services.NewEnvironmentService,
		services.NewNotificationService,
		services.NewVulnerabilityService,
		services.NewImageUpdateService,
		services.NewImageService,
		services.NewBuildService,
		services.NewBuildWorkspaceService,
		services.NewLifecycleService,
		provideProjectServiceInternal,
		services.NewContainerService,
		services.NewDashboardService,
		services.NewNetworkService,
		services.NewPortService,
		services.NewSwarmService,
		services.NewTemplateService,
		services.NewOidcService,
		services.NewSystemService,
		services.NewSystemUpgradeService,
		services.NewDiagnosticsService,
		services.NewGitOpsSyncService,
		services.NewWebhookService,
		services.NewVariableService,
		services.NewHostShellService,

		// Adapters for scalar config fields, unexported parameters, builders, and lifecycle hooks.
		provideVersionServiceInternal,
		provideGitRepositoryServiceInternal,
		provideVolumeServiceInternal,
		provideAuthServiceInternal,
		provideContainerRegistryServiceInternal,
		provideUpdaterServiceInternal,
		provideUserServiceInternal,
		provideApiKeyServiceInternal,
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
		scheduler.NewAutoHealJob,
		scheduler.NewActivitySweepJob,
	),
)
