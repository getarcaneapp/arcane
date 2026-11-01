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

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/apikey"
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
	"getarcane.dev/arcane/v3/internal/image"
	"getarcane.dev/arcane/v3/internal/imageupdate"
	"getarcane.dev/arcane/v3/internal/job"
	"getarcane.dev/arcane/v3/internal/kv"
	"getarcane.dev/arcane/v3/internal/network"
	"getarcane.dev/arcane/v3/internal/notification"
	"getarcane.dev/arcane/v3/internal/oidc"
	"getarcane.dev/arcane/v3/internal/podman"
	"getarcane.dev/arcane/v3/internal/port"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/registry"
	"getarcane.dev/arcane/v3/internal/role"
	s3domain "getarcane.dev/arcane/v3/internal/s3"
	"getarcane.dev/arcane/v3/internal/scheduler"
	"getarcane.dev/arcane/v3/internal/scheduler/flow"
	"getarcane.dev/arcane/v3/internal/search"
	"getarcane.dev/arcane/v3/internal/session"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/internal/template"
	"getarcane.dev/arcane/v3/internal/updater"
	"getarcane.dev/arcane/v3/internal/user"
	"getarcane.dev/arcane/v3/internal/variable"
	"getarcane.dev/arcane/v3/internal/version"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/internal/vulnerability"
	"getarcane.dev/arcane/v3/internal/webhook"
	arcaneoidc "getarcane.dev/arcane/v3/pkg/oidc"
)

type (
	graphParams struct {
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
		JWKSetManager     *arcaneoidc.KeySetManager

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
)

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
			provideDockerClientService(dockerClientServiceParams{
				Ctx:           t.Context(),
				Lifecycle:     lifecycle,
				DB:            databaseDB,
				LocalSettings: settingsService,
				EventService:  eventService,
			})
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
