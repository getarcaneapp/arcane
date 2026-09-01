package di

import (
	"context"
	"net/http"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/backup"
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
	"github.com/getarcaneapp/arcane/backend/v2/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v2/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v2/internal/session"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/template"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"

	"github.com/getarcaneapp/arcane/backend/v2/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v2/internal/build"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v2/internal/network"
	"github.com/getarcaneapp/arcane/backend/v2/internal/oidc"
	"github.com/getarcaneapp/arcane/backend/v2/internal/port"
	"github.com/getarcaneapp/arcane/backend/v2/internal/search"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/internal/systembackup"
	"github.com/getarcaneapp/arcane/backend/v2/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v2/internal/version"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/webhook"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/oidcjwk"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

type graphParams struct {
	fx.In

	AppImages         *appimages.ApplicationImagesService
	User              *user.UserService
	Project           *project.ProjectService
	Environment       *environment.EnvironmentService
	Settings          *settings.SettingsService
	KV                *kv.KVService
	JobSchedule       *job.JobService
	Search            *search.Module
	Container         *container.Module
	Image             *image.ImageService
	Build             *build.BuildService
	BuildWorkspace    *build.BuildWorkspaceService
	Lifecycle         *project.LifecycleService
	Volume            *volume.Module
	BackupEngine      *backup.Engine
	S3Destination     *s3domain.Module
	SystemBackup      *systembackup.Module
	Network           *network.NetworkService
	Port              *port.PortService
	Swarm             *swarm.SwarmService
	ImageUpdate       *imageupdate.ImageUpdateService
	Session           *session.SessionService
	Auth              *auth.AuthService
	Oidc              *oidc.OidcService
	Docker            *docker.DockerClientService
	Template          *template.TemplateService
	ContainerRegistry *registry.ContainerRegistryService
	System            *system.Module
	SystemUpgrade     *system.SystemUpgradeService
	Diagnostics       *diagnostics.DiagnosticsService
	Updater           *updater.Module
	Event             *event.EventService
	Activity          *activity.ActivityService
	Version           *version.VersionService
	Notification      *notification.NotificationService
	ApiKey            *apikey.ApiKeyService
	Federated         *federated.FederatedCredentialService
	GitRepository     *gitrepo.GitRepositoryService
	GitOpsSync        *gitops.GitOpsSyncService
	Webhook           *webhook.Module
	Vulnerability     *vulnerability.VulnerabilityService
	Dashboard         *dashboard.Module
	Role              *role.RoleService
	Variable          *variable.VariableService
	AuthMiddleware    *auth.AuthMiddleware
	JWKSetManager     *oidcjwk.KeySetManager

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
		ActorOptions,
		ServiceOptions,
		JobOptions,
		fx.Invoke(func(graphParams) {}),
	)
	require.NoError(t, err)
}
