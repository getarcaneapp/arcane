//go:build wireinject

// This file is consumed only by wire's code generator (build tag wireinject)
// and is excluded from normal builds. Run `go generate ./internal/di/` after
// changing the provider set to regenerate wire_gen.go.

package di

import (
	"context"
	"net/http"

	"github.com/google/wire"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
)

// ServiceSet is the single, central provider set for the whole backend. Every
// service constructor (or the wrapper it requires) is listed here exactly once;
// wire derives the construction order from the dependency graph, so ordering is
// no longer maintained by hand. wire.Struct assembles the aggregate Services.
var ServiceSet = wire.NewSet(
	// Infra providers that are not themselves services.
	provideResourcesFSInternal,

	// Services wire constructs directly via their real constructors.
	services.NewEventService,
	services.NewActivityService,
	services.NewSettingsService, // returns (*SettingsService, error); wire threads the error.
	services.NewKVService,
	services.NewJobService,
	services.NewSettingsSearchService,
	services.NewCustomizeSearchService,
	services.NewApplicationImagesService,
	services.NewDockerClientService,
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

	// Services that require a wrapper (scalar config field, unexported parameter,
	// or post-construction RoleService builder). See providers.go.
	provideVersionServiceInternal,
	provideGitRepositoryServiceInternal,
	provideVolumeServiceInternal,
	provideAuthServiceInternal,
	provideContainerRegistryServiceInternal,
	provideUpdaterServiceInternal,
	provideUserServiceInternal,
	provideApiKeyServiceInternal,
	provideFederatedCredentialServiceInternal,

	// Shared Echo auth middleware (built from the auth-related services + config).
	provideAuthMiddlewareInternal,

	// Assemble the aggregate container from everything above.
	wire.Struct(new(Services), "*"),
)

// InitializeServices builds the full service graph. ctx, db, cfg, and httpClient
// are graph inputs supplied by the caller; every other value is constructed by
// ServiceSet. The generated implementation lives in wire_gen.go.
func InitializeServices(ctx context.Context, db *database.DB, cfg *config.Config, httpClient *http.Client) (*Services, error) {
	panic(wire.Build(ServiceSet))
}

// JobSet builds every scheduler job from an already-constructed *Services (whose
// fields are exposed to the graph via wire.FieldsOf) plus the app context and config.
var JobSet = wire.NewSet(
	wire.FieldsOf(new(*Services),
		"Updater", "Settings", "ImageUpdate", "Environment", "Docker", "KV",
		"Event", "Activity", "Session", "System", "Notification", "Project",
		"Template", "Vulnerability", "Volume",
	),
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
	wire.Struct(new(Jobs), "*"),
)

// InitializeJobs constructs all scheduler jobs from the built services. The caller
// registers them with the scheduler and wires the settings-change callbacks.
func InitializeJobs(ctx context.Context, cfg *config.Config, svcs *Services) *Jobs {
	panic(wire.Build(JobSet))
}
