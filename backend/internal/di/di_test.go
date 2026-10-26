package di

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/appimages"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/build"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/dashboard"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/diagnostics"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/federated"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitrepo"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v3/internal/job"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/network"
	"github.com/getarcaneapp/arcane/backend/v3/internal/notification"
	"github.com/getarcaneapp/arcane/backend/v3/internal/oidc"
	"github.com/getarcaneapp/arcane/backend/v3/internal/podman"
	"github.com/getarcaneapp/arcane/backend/v3/internal/port"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/registry"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	s3domain "github.com/getarcaneapp/arcane/backend/v3/internal/s3"
	"github.com/getarcaneapp/arcane/backend/v3/internal/search"
	"github.com/getarcaneapp/arcane/backend/v3/internal/session"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/internal/template"
	"github.com/getarcaneapp/arcane/backend/v3/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v3/internal/version"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v3/internal/webhook"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/oidcjwk"
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
	Lifecycle         *project.LifecycleService
	Volume            *volume.Module
	BackupEngine      *backup.Engine
	S3Destination     *s3domain.Module
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
	Dashboard         *dashboard.DashboardService
	Podman            *podman.Module
	Role              *role.RoleService
	Variable          *variable.VariableService
	AuthMiddleware    *auth.AuthMiddleware
	JWKSetManager     *oidcjwk.KeySetManager

	AutoUpdate             *flow.Job `name:"auto-update"`
	ImageUpdateWatcher     *scheduler.ImageUpdateWatcher
	DockerClientRefresh    *scheduler.DockerClientRefreshJob
	Analytics              *scheduler.AnalyticsJob
	EventCleanup           *scheduler.EventCleanupJob
	PruningVolumeHelper    *scheduler.PruningVolumeHelperJob
	ExpiredSessionsCleanup *scheduler.ExpiredSessionsCleanupJob
	ScheduledPrune         *scheduler.ScheduledPruneJob
	FilesystemWatcher      *scheduler.FilesystemWatcherJob
	VulnerabilityScan      *flow.Job `name:"vulnerability-scan"`
	AutoHeal               *scheduler.AutoHealJob
}

func TestOptionsValidate(t *testing.T) {
	_, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	err := fx.ValidateApp(
		fx.Supply(
			cancel,
			&config.Config{},
			settings.Env{},
			(*database.DB)(nil),
			&http.Client{},
			fx.Annotated{Name: "encryptionKey", Target: "test-encryption-key"},
		),
		fx.Provide(func() context.Context { return t.Context() }),
		ActorOptions,
		ServiceOptions,
		JobOptions,
		fx.Invoke(func(graphParams) {}),
	)
	require.NoError(t, err)
}

func TestDockerEventLifecycle(t *testing.T) {
	for _, mode := range []string{"manager", "agent"} {
		t.Run(mode, func(t *testing.T) {
			streamStopped := make(chan struct{})
			var stopped sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/_ping") {
					w.Header().Set("Api-Version", "1.56")
					return
				}
				if !strings.HasSuffix(r.URL.Path, "/events") {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				err := json.MarshalWrite(
					w,
					events.Message{
						Type:   events.ContainerEventType,
						Action: events.ActionStart,
						Actor: events.Actor{
							ID:         "daemon-container",
							Attributes: map[string]string{"name": "external"},
						},
					},
				)
				if err != nil {
					return
				}
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				stopped.Do(func() { close(streamStopped) })
			}))
			t.Cleanup(server.Close)
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, db.AutoMigrate(&event.Event{}, &settings.SettingVariable{}))
			databaseDB := &database.DB{DB: db}
			cfg := &config.Config{AgentEnabled: mode == "agent"}
			settingsService, err := settings.NewSettingsService(t.Context(), databaseDB, cfg, settings.Env{"dockerHost": server.URL})
			require.NoError(t, err)
			lifecycle := fxtest.NewLifecycle(t)
			eventService := event.NewEventService(databaseDB, cfg, nil)
			provideDockerClientService(t.Context(), lifecycle, databaseDB, settingsService, eventService)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
				defer cancel()
				require.NoError(t, lifecycle.Stop(ctx))
			})
			require.NoError(t, lifecycle.Start(t.Context()))
			require.Eventually(t, func() bool {
				var count int64
				return db.Model(&event.Event{}).Count(&count).Error == nil && count == 1
			}, 2*time.Second, 10*time.Millisecond)
			var record event.Event
			require.NoError(t, db.First(&record).Error)
			require.Equal(t, event.EventTypeContainerStart, record.Type)
			require.Equal(t, "System", *record.Username)
			require.Equal(t, "0", *record.EnvironmentID)
			require.Equal(t, "docker", record.Metadata["source"])
			stopCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			require.NoError(t, lifecycle.Stop(stopCtx))
			select {
			case <-streamStopped:
			case <-stopCtx.Done():
				t.Fatal("Docker event stream was not joined")
			}
		})
	}
}
