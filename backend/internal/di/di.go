// Package di owns the backend's dependency-injection graph.
package di

import (
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"go.uber.org/fx"
)

// ServiceOptions provides the backend service graph.
var ServiceOptions = fx.Options(
	fx.Provide(
		// Infrastructure values consumed by services.
		provideResourcesFSInternal,

		// Services constructed directly through their public constructors.
		services.NewEventService,
		services.NewActivityService,
		services.NewSettingsService,
		services.NewKVService,
		services.NewJobService,
		services.NewSettingsSearchService,
		services.NewCustomizeSearchService,
		services.NewApplicationImagesService,
		provideDockerClientServiceInternal,
		services.NewRoleService,
		services.NewSessionService,
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
		provideAnalyticsJobInternal,
		scheduler.NewEventCleanupJob,
		scheduler.NewPruningVolumeHelperJob,
		scheduler.NewExpiredSessionsCleanupJob,
		scheduler.NewScheduledPruneJob,
		provideFilesystemWatcherJobInternal,
		scheduler.NewVulnerabilityScanJob,
		scheduler.NewAutoHealJob,
	),
)
