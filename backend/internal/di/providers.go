package di

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"uuid"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/events"
	"go.getarcane.app/streams/bus"
	"go.uber.org/fx"

	"github.com/getarcaneapp/arcane/backend/v3/internal/activity"
	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/backup"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/event"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/imageupdate"
	"github.com/getarcaneapp/arcane/backend/v3/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/system"
	"github.com/getarcaneapp/arcane/backend/v3/internal/template"
	"github.com/getarcaneapp/arcane/backend/v3/internal/updater"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/authz"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/agent"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/scheduler/runs"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/francis"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/oidcjwk"
)

type backupEngineParams struct {
	fx.In

	Lifecycle    fx.Lifecycle
	Admission    *runs.Admission
	ImageService *image.ImageService
	Roles        *role.RoleService
	Config       *config.Config
}

func provideBackupEngine(p backupEngineParams) *backup.Engine {
	engine := backup.NewEngine(p.Admission, p.ImageService)
	engine.SetAuthorize(func(ctx context.Context, requester types.BackupRequester) error {
		if p.Config.AgentEnabled && requester.UserID == "agent" && requester.APIKeyID == "" {
			return nil
		}
		if requester.UserID == "" {
			return errors.New("requesting user unavailable")
		}
		permissions, err := p.Roles.ResolveExecutionPermissions(ctx, requester.UserID, requester.APIKeyID)
		if err != nil {
			return err
		}
		if !permissions.Allows(requester.Permission, requester.EnvironmentID) {
			return errors.New("requesting user no longer has permission for this backup")
		}
		if requester.Permission == authz.PermSystemBackupsManage && !permissions.IsGlobalAdmin() {
			return errors.New("system backups require a global administrator")
		}
		return nil
	})
	p.Lifecycle.Append(fx.Hook{OnStop: engine.Stop})
	return engine
}

type actorRuntimeParams struct {
	fx.In

	AppCtx          context.Context
	Config          *config.Config
	SettingsService *settings.SettingsService
	Lifecycle       fx.Lifecycle
	CancelApp       context.CancelCauseFunc
	EncryptionKey   string `name:"encryptionKey"`
}

func provideActorRuntime(p actorRuntimeParams) (*francis.Runtime, error) {
	runtime, err := francis.New(francis.Config{
		DatabaseURL: p.Config.DatabaseURL,
		Port:        p.Config.ActorPort,
	})
	if err != nil {
		return nil, err
	}
	p.Lifecycle.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		if configureIdentityErr := runtime.ConfigureIdentity(p.EncryptionKey, p.SettingsService.GetSettingsConfig().InstanceID.Value); configureIdentityErr != nil {
			return configureIdentityErr
		}
		return runtime.Start(ctx, p.AppCtx, func(err error) {
			slog.ErrorContext(p.AppCtx, "Francis host failed", "error", err)
			p.CancelApp(fmt.Errorf("francis host failed: %w", err))
		})
	}, OnStop: runtime.Stop})
	return runtime, nil
}

func provideRunCoordinator(runtime *francis.Runtime, store *kv.KVService, cfg *config.Config) (*runs.Coordinator, error) {
	coordinator := runs.New(store, runtime.Service(), cfg.GetLocation())
	if err := coordinator.Register(runtime); err != nil {
		return nil, err
	}
	return coordinator, nil
}

type workflowEngineParams struct {
	fx.In

	AppCtx      context.Context
	Runtime     *francis.Runtime
	Coordinator *runs.Coordinator
	Activities  *activity.ActivityService
	Lifecycle   fx.Lifecycle
}

// provideWorkflowEngine starts monitoring after the host is ready; its
// hook follows the runtime's because the engine depends on it.
func provideWorkflowEngine(p workflowEngineParams) (*flow.Engine, error) {
	engine, err := flow.New(p.AppCtx, p.Runtime, p.Coordinator, p.Activities)
	if err != nil {
		return nil, err
	}
	p.Lifecycle.Append(fx.Hook{OnStart: engine.Start, OnStop: engine.Stop})
	return engine, nil
}

func provideAdmissionGate(runtime *francis.Runtime) (*runs.Admission, error) {
	admission := runs.NewAdmission(runtime.Service(), uuid.New().String())
	if err := admission.Register(runtime); err != nil {
		return nil, err
	}
	return admission, nil
}

func provideTunnelRegistry(lc fx.Lifecycle) *agent.TunnelRegistry {
	localRegistry := agent.NewTunnelRegistry()
	agent.SetDefaultRegistry(localRegistry)
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		defer agent.ClearDefaultRegistry(localRegistry)
		stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return localRegistry.Stop(stopCtx)
	}})
	return localRegistry
}

type dockerClientServiceParams struct {
	fx.In

	Ctx           context.Context
	Lifecycle     fx.Lifecycle
	DB            *database.DB
	LocalSettings *settings.SettingsService
	EventService  *event.EventService
}

func provideDockerClientService(p dockerClientServiceParams) *docker.DockerClientService {
	service := docker.NewDockerClientService(p.Ctx, p.DB, p.LocalSettings, bus.Config{OnDrop: func(message events.Message) {
		// Overflow is recovered synchronously, applying backpressure instead of losing log entries.
		p.EventService.RecordDockerEvent(p.Ctx, message)
	}})
	var stopStream, stopLog func(context.Context) error
	var unsubscribe func()
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(startCtx context.Context) error {
			run, cleanup := p.EventService.SubscribeDockerEvents(service.EventBus())
			unsubscribe = cleanup
			var err error
			stopLog, err = concurrency.StartSupervised(p.Ctx, "Docker event log", run)
			if err != nil {
				cleanup()
				return err
			}
			stopStream, err = concurrency.StartSupervised(p.Ctx, "Docker event watcher", func(runCtx context.Context) error {
				service.WatchEvents(runCtx)
				return nil
			})
			if err != nil {
				err = errors.Join(err, stopLog(startCtx))
				cleanup()
			}
			return err
		},
		OnStop: func(stopCtx context.Context) error {
			var err error
			if stopStream != nil {
				err = stopStream(stopCtx)
			}
			if stopLog != nil {
				err = errors.Join(err, stopLog(stopCtx))
			}
			if unsubscribe != nil {
				unsubscribe()
			}
			service.Close()
			return err
		},
	})
	return service
}

// provideImageUpdateService registers the image-check workflow while the host is still unstarted.
func provideImageUpdateService(p imageupdate.ImageUpdateServiceParams, engine *flow.Engine) (*imageupdate.ImageUpdateService, error) {
	service := imageupdate.NewImageUpdateService(p)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideImageService registers the auto-patch workflow while the host is still unstarted.
func provideImageService(p image.ImageServiceParams, engine *flow.Engine) (*image.ImageService, error) {
	service := image.NewImageService(p)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideVulnerabilityService registers the scheduled scan workflow while the host is still unstarted.
func provideVulnerabilityService(p vulnerability.VulnerabilityServiceParams, engine *flow.Engine) (*vulnerability.VulnerabilityService, error) {
	service := vulnerability.NewVulnerabilityService(p)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

func provideUpdaterService(p updater.UpdaterServiceParams, engine *flow.Engine) (*updater.UpdaterService, error) {
	service, err := updater.NewUpdaterService(p)
	if err != nil {
		return nil, err
	}
	if registerWorkflowsErr := service.RegisterWorkflows(engine); registerWorkflowsErr != nil {
		return nil, registerWorkflowsErr
	}
	return service, nil
}

func provideJWKSetManager(ctx context.Context, lc fx.Lifecycle) *oidcjwk.KeySetManager {
	manager := oidcjwk.NewKeySetManager(ctx)
	lc.Append(fx.Hook{OnStop: manager.Shutdown})
	return manager
}

type authMiddlewareParams struct {
	fx.In

	AuthService *auth.AuthService
	APIKey      *apikey.ApiKeyService
	Env         *environment.EnvironmentService
	LocalRole   *role.RoleService
	Config      *config.Config
}

func provideAuthMiddleware(p authMiddlewareParams) *auth.AuthMiddleware {
	return auth.NewAuthMiddleware(p.AuthService, p.Config).
		WithApiKeyValidator(p.APIKey).
		WithEnvironmentAccessTokenResolver(p.Env).
		WithPermissionResolver(p.LocalRole)
}

type filesystemWatcherJobParams struct {
	fx.In

	Ctx           context.Context
	Lifecycle     fx.Lifecycle
	LocalProject  *project.ProjectService
	LocalTemplate *template.TemplateService
	LocalSettings *settings.SettingsService
}

func provideFilesystemWatcherJob(p filesystemWatcherJobParams) (
	*scheduler.FilesystemWatcherJob,
	error,
) {
	job, err := scheduler.NewFilesystemWatcherJob(p.Ctx, p.LocalProject, p.LocalTemplate, p.LocalSettings)
	if err != nil {
		return nil, err
	}
	var stop func(context.Context) error
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			stop, err = concurrency.StartSupervised(p.Ctx, "filesystem watcher", job.Supervise)
			if err != nil {
				return err
			}
			slog.InfoContext(p.Ctx, "Filesystem watcher job registered")
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			var stopWorkerErr error
			if stop != nil {
				stopWorkerErr = stop(stopCtx)
			}
			return errors.Join(stopWorkerErr, job.Stop(stopCtx))
		},
	})
	return job, nil
}

// provideGitOpsSyncService registers the gitops-sync workflow while the host is still unstarted.
func provideGitOpsSyncService(p gitops.GitOpsSyncServiceParams, engine *flow.Engine) (*gitops.GitOpsSyncService, error) {
	service := gitops.NewGitOpsSyncService(p)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	return service, nil
}

// provideVolumeService registers the volume backup workflows while the host is still unstarted.
func provideVolumeService(lc fx.Lifecycle, p volume.VolumeServiceParams, engine *flow.Engine) (*volume.VolumeService, error) {
	service := volume.NewVolumeService(p)
	if err := service.RegisterWorkflows(engine); err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		cleanupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		service.CleanupHelperContainers(cleanupCtx)
		return nil
	}})
	return service, nil
}

// provideSystemService registers the system backup workflows while the host is still unstarted.
func provideSystemService(
	p system.SystemServiceParams,
	environments *environment.EnvironmentService,
	engine *flow.Engine,
	roles *role.RoleService,
) (*system.SystemService, error) {
	service := system.NewSystemService(p)
	if err := service.RegisterWorkflows(engine, environments, roles); err != nil {
		return nil, err
	}
	return service, nil
}
