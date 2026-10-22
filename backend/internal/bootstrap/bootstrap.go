package bootstrap

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/labstack/echo/v5"
	"github.com/moby/moby/client"
	"github.com/subosito/gotenv"
	"go.getarcane.app/sys/crypto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/getarcaneapp/arcane/backend/v3/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v3/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config"
	"github.com/getarcaneapp/arcane/backend/v3/internal/config/schema"
	"github.com/getarcaneapp/arcane/backend/v3/internal/container"
	"github.com/getarcaneapp/arcane/backend/v3/internal/database"
	"github.com/getarcaneapp/arcane/backend/v3/internal/di"
	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v3/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v3/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v3/internal/image"
	"github.com/getarcaneapp/arcane/backend/v3/internal/project"
	"github.com/getarcaneapp/arcane/backend/v3/internal/role"
	"github.com/getarcaneapp/arcane/backend/v3/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v3/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v3/internal/user"
	"github.com/getarcaneapp/arcane/backend/v3/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v3/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/startup"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/libarcane/timeouts"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/concurrency"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v3/pkg/utils/tracing"
)

func Bootstrap(ctx context.Context) error {
	if err := gotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	if err := schema.CheckLegacyEnv(ctx); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	settingsEnv, err := settings.LoadEnv()
	if err != nil {
		return fmt.Errorf("load settings environment: %w", err)
	}
	if err = startup.ApplyRequestedRuntimeIdentity(ctx, &startup.RuntimeIdentityConfig{
		PUID:              cfg.PUID,
		PGID:              cfg.PGID,
		DockerHost:        startup.ResolveDockerHost(settingsEnv.ValueOrDefault("dockerHost"), os.Stat),
		DockerConfig:      cfg.DockerConfig,
		DatabaseURL:       cfg.DatabaseURL,
		ProjectsDirectory: settingsEnv.ValueOrDefault("projectsDirectory"),
		InContainer:       cfg.InContainer,
	}); err != nil {
		return fmt.Errorf("apply runtime identity: %w", err)
	}

	SetupSlogLogger(cfg, nil)
	database.SetGormLogger(BuildGormLogger(cfg))
	slog.InfoContext(ctx, "Arcane is starting...", "version", config.Version)
	startup.ApplyMemoryLimit(ctx)
	slog.InfoContext(ctx, "Arcane Identity Configuration", "puid", os.Getuid(), "pgid", os.Getgid())

	appCtx, cancelApp := context.WithCancelCause(ctx)
	appCtx = utils.WithAppLifecycleContext(appCtx)

	db, err := database.Initialize(appCtx, cfg.DatabaseURL, database.MigrationOptions{AllowDowngrade: cfg.AllowDowngrade})
	if err != nil {
		cancelApp(nil)
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	slog.InfoContext(appCtx, "Database initialized successfully")
	defer func() {
		cancelApp(nil)
		if closeErr := db.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "Error closing database", "error", closeErr)
		}
	}()

	encryptionKey, err := settings.ResolveEncryptionKey(appCtx, db, cfg)
	if err != nil {
		return fmt.Errorf("resolve encryption key: %w", err)
	}
	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: encryptionKey,
		Environment:   string(cfg.Environment),
		AgentMode:     cfg.AgentMode,
	})

	// Telemetry identifies this installation by its persisted instance ID, so it starts once the
	// database is open but before Docker clients and services. Exporter errors go to the console-only logger.
	instanceID, err := settings.EnsureInstanceID(appCtx, db)
	if err != nil {
		return err
	}
	providers, err := setupTelemetry(ctx, cfg, slog.Default(), instanceID)
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
		defer cancel()
		if shutdownErr := providers.Shutdown(shutdownCtx); shutdownErr != nil {
			slog.ErrorContext(shutdownCtx, "Error shutting down telemetry", "error", shutdownErr)
		}
	}()
	if providers.loggerProvider != nil {
		SetupSlogLogger(cfg, otelslog.NewHandler(tracing.InstrumentationName, otelslog.WithLoggerProvider(providers.loggerProvider)))
		db.Logger = BuildGormLogger(cfg)
	}
	if providers.tracerProvider != nil || providers.meterProvider != nil || providers.loggerProvider != nil {
		slog.InfoContext(ctx, "OpenTelemetry export enabled", "traces", providers.tracerProvider != nil, "metrics", providers.meterProvider != nil, "logs", providers.loggerProvider != nil)
	}

	app := fx.New(applicationOptions(appCtx, cfg, settingsEnv, db, encryptionKey, cancelApp))

	startCtx, cancelStart := context.WithTimeout(ctx, app.StartTimeout())
	defer cancelStart()
	if startErr := app.Start(startCtx); startErr != nil {
		return fmt.Errorf("start application: %w", startErr)
	}

	// appCtx also ends when a component stops the app itself; its cause is then the run error.
	var runErr error
	select {
	case <-appCtx.Done():
		if ctx.Err() == nil {
			runErr = context.Cause(appCtx)
		} else {
			slog.InfoContext(appCtx, "Context canceled")
		}
	case signal := <-app.Done():
		slog.InfoContext(appCtx, "Received shutdown signal", "signal", signal)
	}

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), app.StopTimeout())
	defer cancelStop()
	if stopErr := app.Stop(stopCtx); stopErr != nil {
		return errors.Join(runErr, fmt.Errorf("stop application: %w", stopErr))
	}

	slog.InfoContext(context.WithoutCancel(appCtx), "Arcane shutdown complete")
	return runErr
}

func applicationOptions(appCtx context.Context, cfg *config.Config, settingsEnv settings.Env, db *database.DB, encryptionKey string, cancelApp context.CancelCauseFunc) fx.Option {
	return fx.Options(
		fx.Supply(
			cfg,
			settingsEnv,
			db,
			cancelApp,
			fx.Annotated{Name: "encryptionKey", Target: encryptionKey},
		),
		fx.Provide(
			func() context.Context { return appCtx },
			func(settingsService *settings.SettingsService) *http.Client {
				return httpx.NewHTTPClient(types.HTTPClientOptions{
					Timeout:             timeouts.GetDuration(settingsService.GetSettingsOrDefaults(appCtx).HTTPClientTimeout.AsInt(), timeouts.DefaultHTTPClient),
					TLSHandshakeTimeout: 10 * time.Second,
				})
			},
		),
		di.ActorOptions,
		di.ServiceOptions,
		di.JobOptions,
		serverOptions,
		fx.Invoke(
			registerAppCancelHook,
			initializeStartupState,
			registerJobs,
			startEdgeTunnelClient,
			startGlobalVariableSync,
			startBackgroundStartupTasks,
			startProjectStatusWatcher,
		),
		fx.WithLogger(func() fxevent.Logger {
			logger := &fxevent.SlogLogger{Logger: slog.Default()}
			logger.UseLogLevel(slog.LevelDebug)
			return logger
		}),
		fx.StartTimeout(5*time.Minute),
		fx.StopTimeout(7*time.Second),
	)
}

type initializeStartupStateParams struct {
	fx.In

	AppCtx context.Context
	Config *config.Config
	DB     *database.DB

	Volume      *volume.VolumeService
	Settings    *settings.SettingsService
	Environment *environment.EnvironmentService
	GitOpsSync  *gitops.GitOpsSyncService
	Project     *project.ProjectService
	Container   *container.ContainerService
	Image       *image.ImageService
	Variable    *variable.VariableService
	Role        *role.RoleService
	Auth        *auth.AuthService
	User        *user.UserService
	ApiKey      *apikey.ApiKeyService
}

const startupTaskTimeout = 30 * time.Second

func initializeStartupState(p initializeStartupStateParams) {
	appCtx := p.AppCtx
	cfg := p.Config

	helperCtx, cancelHelpers := context.WithTimeout(appCtx, startupTaskTimeout)
	startup.CleanupOrphanedVolumeHelpers(helperCtx, p.Volume.CleanupOrphanedVolumeHelpers)
	cancelHelpers()

	// Startup repairs are best-effort: log each failure and continue.
	for _, task := range []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "normalize projects directory", run: p.Settings.NormalizeProjectsDirectory},
		{name: "normalize builds directory", run: p.Settings.NormalizeBuildsDirectory},
		{name: "encrypt stored sensitive settings", run: p.Settings.EncryptStoredSensitiveSettings},
		{name: "ensure local environment", run: func(ctx context.Context) error {
			return p.Environment.EnsureLocalEnvironment(ctx, cmp.Or(cfg.AppUrl, "http://localhost:"+cfg.Port))
		}},
		{name: "clean up orphaned GitOps syncs", run: p.GitOpsSync.CleanupOrphanedSyncsOnStartup},
		{name: "clean up leaked GitOps scratch directories", run: p.GitOpsSync.CleanupLeakedScratchDirsOnStartup},
		{name: "clean up leaked git clone directories", run: p.GitOpsSync.CleanupLeakedCloneDirsOnStartup},
		{name: "reconcile directory GitOps projects", run: p.GitOpsSync.ReconcileDirectorySyncProjectsOnStartup},
		{name: "recover interrupted project renames", run: p.Project.RecoverProjectRenameJournals},
		{name: "recover interrupted container redeploys", run: p.Container.RecoverInterruptedRedeploys},
		{name: "recover stuck project statuses", run: p.Project.RecoverStuckProjectStatuses},
		{name: "fail interrupted image patches", run: p.Image.RecoverInterruptedPatches},
	} {
		taskCtx, cancelTask := context.WithTimeout(appCtx, startupTaskTimeout)
		if err := task.run(taskCtx); err != nil {
			slog.WarnContext(appCtx, "Startup task failed", "task", task.name, "error", err)
		}
		cancelTask()
	}

	if !cfg.AgentMode {
		if err := p.Environment.ReconcileEdgeStatusesOnStartup(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to reconcile edge environment statuses on startup", "error", err)
		}
		p.Environment.RegisterMetrics()

		p.Environment.SetVariableSyncer(p.Variable)
		p.Environment.SetUserInvalidator(func(userID string) {
			p.Role.InvalidateUser(userID)
			p.Auth.InvalidateUserTokenCache(userID)
		})
	}

	startup.InitializeNonAgentFeatures(appCtx, cfg.AgentMode,
		p.Role.EnsureBuiltInRoles,
		func(ctx context.Context) error {
			// Backfill legacy users.roles first so CreateDefaultAdmin's zero-global-admin gate sees them;
			// the role startup tasks below repeat it for agent mode and retries.
			if err := p.Role.BackfillLegacyRoleAssignments(ctx); err != nil {
				slog.WarnContext(ctx, "Failed to backfill legacy role assignments before admin bootstrap", "error", err)
			}
			return p.User.CreateDefaultAdmin(ctx)
		},
		func(ctx context.Context) error {
			return p.ApiKey.ReconcileDefaultAdminAPIKey(ctx, cfg.AdminStaticAPIKey)
		},
		func(ctx context.Context) error {
			startup.InitializeAutoLogin(ctx, cfg.AutoLoginUsername)
			return nil
		},
	)
	startup.CleanupUnknownSettings(appCtx, p.Settings)

	if err := p.Role.EnsureBuiltInRoles(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to reconcile built-in roles", "error", err)
	}
	// Runs after EnsureBuiltInRoles seeds role IDs and before the API key backfill and admin guard read assignments.
	if err := p.Role.BackfillLegacyRoleAssignments(appCtx); err != nil {
		slog.ErrorContext(appCtx, "Failed to backfill legacy users.roles into user_role_assignments", "error", err)
	}
	if err := p.ApiKey.BackfillApiKeyPermissions(appCtx); err != nil {
		slog.WarnContext(appCtx, "Failed to backfill API key permissions", "error", err)
	}
	if err := p.Role.ReconcileEnvOidcMappings(appCtx, cfg.OidcRoleMappings); err != nil {
		slog.ErrorContext(appCtx, "Failed to reconcile ARCANE_OIDC_ROLE_MAPPINGS", "error", err)
	}
	if !cfg.AgentMode {
		if err := p.Role.AssertGlobalAdminExists(appCtx, p.DB.WithContext(appCtx)); err != nil {
			slog.ErrorContext(appCtx, "RBAC global admin guard failed", "error", err)
		}
	}

	// Only edge agents auto-pair; direct agents are passive and the manager dials them.
	if agentToken := p.Settings.AgentToken(appCtx); cfg.AgentMode && cfg.EdgeAgent && agentToken != "" && cfg.ManagerApiUrl != "" {
		if err := handleAgentBootstrapPairing(appCtx, cfg, agentToken); err != nil {
			slog.WarnContext(appCtx, "Failed to auto-pair agent with manager", "error", err)
		}
	} else if cfg.AgentMode && !cfg.EdgeAgent {
		slog.InfoContext(appCtx, "Direct mode active: agent operates as a passive HTTP server; no outbound connection to manager required")
	}
}

func startEdgeTunnelClient(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, settingsService *settings.SettingsService, router *echo.Echo, _ *http.Server) {
	var stop func(context.Context) error
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			agentToken := settingsService.AgentToken(appCtx)
			if !cfg.EdgeAgent || cfg.ManagerApiUrl == "" || agentToken == "" {
				return nil
			}
			tunnelStop, err := edge.StartTunnelClient(appCtx, buildEdgeRuntimeConfig(cfg, agentToken), router)
			if err != nil {
				slog.ErrorContext(appCtx, "Failed to start edge tunnel client", "error", err)
				return nil
			}
			stop = tunnelStop
			slog.InfoContext(appCtx, "Edge tunnel client started", "managerUrl", cfg.ManagerApiUrl)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if stop == nil {
				return nil
			}
			return stop(ctx)
		},
	})
}

func startGlobalVariableSync(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, variables *variable.VariableService, _ *http.Server) {
	if cfg.AgentMode {
		return
	}
	lc.Append(fx.Hook{OnStart: func(context.Context) error {
		go variables.SyncAll(appCtx)
		return nil
	}})
}

func startBackgroundStartupTasks(appCtx context.Context, lc fx.Lifecycle, dockerService *docker.DockerClientService, swarmService *swarm.SwarmService, _ *http.Server) {
	lc.Append(fx.Hook{OnStart: func(context.Context) error {
		go func() {
			dockerCtx, cancelDocker := context.WithTimeout(appCtx, startupTaskTimeout)
			defer cancelDocker()
			startup.TestDockerConnection(dockerCtx, func(ctx context.Context) error {
				_, err := dockerService.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (struct{}, error) {
					version, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
					if err != nil {
						return struct{}{}, err
					}

					effectiveAPIVersion := cmp.Or(strings.TrimSpace(dockerClient.ClientVersion()), strings.TrimSpace(version.APIVersion))
					slog.InfoContext(ctx, "Docker API versions detected", "clientApiVersion", dockerClient.ClientVersion(), "serverApiVersion", version.APIVersion, "effectiveApiVersion", effectiveAPIVersion)
					return struct{}{}, nil
				})
				return err
			})
			if err := swarmService.SyncSwarmEnabledState(dockerCtx); err != nil {
				slog.WarnContext(appCtx, "Failed to persist swarm enabled state", "error", err)
			}
		}()
		return nil
	}})
}

func startProjectStatusWatcher(appCtx context.Context, lc fx.Lifecycle, projectService *project.ProjectService, _ *http.Server) {
	var stop func(context.Context) error
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var err error
			stop, err = concurrency.StartSupervised(appCtx, "project status watcher", projectService.WatchStatuses)
			return err
		},
		OnStop: func(stopCtx context.Context) error {
			if stop == nil {
				return nil
			}
			return stop(stopCtx)
		},
	})
}

func registerAppCancelHook(lc fx.Lifecycle, cancelApp context.CancelCauseFunc) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cancelApp(nil)
			return nil
		},
	})
}

func handleAgentBootstrapPairing(ctx context.Context, cfg *config.Config, agentToken string) error {
	slog.InfoContext(ctx, "Agent mode detected with token, attempting auto-pairing", "managerUrl", cfg.ManagerApiUrl)

	pairURL := strings.TrimRight(httpx.ManagerBaseURL(cfg.ManagerApiUrl), "/") + "/api/environments/pair"

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pairURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create pairing request: %w", err)
	}

	req.Header.Set("X-Api-Key", agentToken)

	httpClient, err := edge.NewManagerHTTPClient(buildEdgeRuntimeConfig(cfg, agentToken), 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to configure edge pairing client: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pairing request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	switch {
	case resp.StatusCode == http.StatusOK:
		slog.InfoContext(ctx, "Successfully paired agent with manager", "managerUrl", cfg.ManagerApiUrl)
		return nil
	case resp.StatusCode == http.StatusBadRequest && strings.Contains(string(body), "not in pending status"):
		slog.InfoContext(ctx, "Agent already paired with manager", "managerUrl", cfg.ManagerApiUrl)
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		// Not fatal: the key may belong to an earlier pairing, and a configured token still works.
		slog.DebugContext(ctx, "Pairing skipped - API key not recognized (agent may already be paired)", "managerUrl", cfg.ManagerApiUrl)
		return nil
	}
	return fmt.Errorf("pairing failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
