package di

import (
	"context"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

type graphParams struct {
	fx.In

	AppImages         *services.ApplicationImagesService
	User              *services.UserService
	Project           *services.ProjectService
	Environment       *services.EnvironmentService
	Settings          *services.SettingsService
	KV                *services.KVService
	JobSchedule       *services.JobService
	SettingsSearch    *services.SettingsSearchService
	CustomizeSearch   *services.CustomizeSearchService
	Container         *services.ContainerService
	Image             *services.ImageService
	Build             *services.BuildService
	BuildWorkspace    *services.BuildWorkspaceService
	Lifecycle         *services.LifecycleService
	Volume            *services.VolumeService
	Network           *services.NetworkService
	Port              *services.PortService
	Swarm             *services.SwarmService
	ImageUpdate       *services.ImageUpdateService
	Session           *services.SessionService
	Auth              *services.AuthService
	Oidc              *services.OidcService
	Docker            *services.DockerClientService
	Template          *services.TemplateService
	ContainerRegistry *services.ContainerRegistryService
	System            *services.SystemService
	SystemUpgrade     *services.SystemUpgradeService
	Diagnostics       *services.DiagnosticsService
	Updater           *services.UpdaterService
	Event             *services.EventService
	Activity          *services.ActivityService
	Version           *services.VersionService
	Notification      *services.NotificationService
	ApiKey            *services.ApiKeyService
	Federated         *services.FederatedCredentialService
	GitRepository     *services.GitRepositoryService
	GitOpsSync        *services.GitOpsSyncService
	Webhook           *services.WebhookService
	Vulnerability     *services.VulnerabilityService
	Dashboard         *services.DashboardService
	Role              *services.RoleService
	Variable          *services.VariableService
	AuthMiddleware    *middleware.AuthMiddleware

	AutoUpdate             *scheduler.AutoUpdateJob
	ImageUpdateWatcher     *scheduler.ImageUpdateWatcher
	DockerClientRefresh    *scheduler.DockerClientRefreshJob
	Analytics              *scheduler.AnalyticsJob
	EventCleanup           *scheduler.EventCleanupJob
	PruningVolumeHelper    *scheduler.PruningVolumeHelperJob
	ExpiredSessionsCleanup *scheduler.ExpiredSessionsCleanupJob
	ScheduledPrune         *scheduler.ScheduledPruneJob
	FilesystemWatcher      *scheduler.FilesystemWatcherJob
	VulnerabilityScan      *scheduler.VulnerabilityScanJob
	AutoHeal               *scheduler.AutoHealJob
}

func TestOptionsValidate(t *testing.T) {
	err := fx.ValidateApp(
		fx.Supply(
			&config.Config{},
			(*database.DB)(nil),
			&http.Client{},
		),
		fx.Provide(func() context.Context { return context.Background() }),
		ServiceOptions,
		JobOptions,
		fx.Invoke(func(graphParams) {}),
	)
	require.NoError(t, err)
}
