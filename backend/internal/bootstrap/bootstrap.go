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

	httpxtypes "github.com/getarcaneapp/arcane/types/v2/httpx"
	"github.com/labstack/echo/v5"
	"github.com/moby/moby/client"
	"github.com/subosito/gotenv"
	kit "go.getarcane.app/kit/pkg"
	"go.getarcane.app/sys/crypto"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/getarcaneapp/arcane/backend/v2/internal/apikey"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/di"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/role"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/user"
	"github.com/getarcaneapp/arcane/backend/v2/internal/variable"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	"github.com/getarcaneapp/arcane/backend/v2/internal/vulnerability"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/startup"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/tracing"
)

func Bootstrap(ctx context.Context) error {
	if err := gotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg := config.Load()
	runtimeIdentityCfg := &startup.RuntimeIdentityConfig{
		PUID:              cfg.PUID,
		PGID:              cfg.PGID,
		DockerHost:        cfg.DockerHost,
		DockerConfig:      cfg.DockerConfig,
		DatabaseURL:       cfg.DatabaseURL,
		ProjectsDirectory: cfg.ProjectsDirectory,
	}
	if err := startup.ApplyRequestedRuntimeIdentity(ctx, runtimeIdentityCfg); err != nil {
		return fmt.Errorf("apply runtime identity: %w", err)
	}
	cfg.DockerConfig = runtimeIdentityCfg.DockerConfig

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
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
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

	app := fx.New(applicationOptions(appCtx, cfg, db, cancelApp))

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

func applicationOptions(appCtx context.Context, cfg *config.Config, db *database.DB, cancelApp context.CancelCauseFunc) fx.Option {
	return fx.Options(
		fx.Supply(cfg, db, cancelApp),
		fx.Provide(
			func() context.Context { return appCtx },
			func(cfg *config.Config) *http.Client {
				return httpx.NewHTTPClient(httpxtypes.ClientOptions{
					Timeout:             kit.Ternary(cfg.HTTPClientTimeout > 0, time.Duration(cfg.HTTPClientTimeout)*time.Second, 10*time.Second),
					TLSHandshakeTimeout: kit.Ternary(cfg.HTTPClientTimeout > 0, 10*time.Second, 5*time.Second),
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
		),
		fx.WithLogger(func() fxevent.Logger {
			logger := &fxevent.SlogLogger{Logger: slog.Default()}
			logger.UseLogLevel(slog.LevelDebug)
			return logger
		}),
		fx.StartTimeout(5*time.Minute),
		fx.StopTimeout(8*time.Second),
	)
}

// isWeakProductionEncryptionKey reports whether a production ENCRYPTION_KEY is an
// unprefixed passphrase shorter than 32 characters.
func isWeakProductionEncryptionKey(encryptionKey, localEnvironment string, agentMode bool) bool {
	if localEnvironment != "production" || agentMode {
		return false
	}
	key := strings.TrimSpace(encryptionKey)
	if key == "" || strings.HasPrefix(key, "hex:") || strings.HasPrefix(key, "base64:") {
		return false
	}
	return len(strings.TrimPrefix(key, "raw:")) < 32
}

type initializeStartupStateParams struct {
	fx.In

	AppCtx context.Context
	Config *config.Config
	DB     *database.DB

	Volume        *volume.VolumeService
	Settings      *settings.SettingsService
	Environment   *environment.EnvironmentService
	GitOpsSync    *gitops.GitOpsSyncService
	Project       *project.ProjectService
	Container     *container.ContainerService
	Image         *image.ImageService
	Variable      *variable.VariableService
	Docker        *docker.DockerClientService
	Swarm         *swarm.SwarmService
	Role          *role.RoleService
	User          *user.UserService
	ApiKey        *apikey.ApiKeyService
	Vulnerability *vulnerability.VulnerabilityService
}

func initializeStartupState(p initializeStartupStateParams) {
	appCtx := p.AppCtx
	cfg := p.Config

	startup.CleanupOrphanedVolumeHelpers(appCtx, p.Volume.CleanupOrphanedVolumeHelpers)

	runtimeCfg := &startup.RuntimeConfig{
		AgentMode:         cfg.AgentMode,
		AgentToken:        cfg.AgentToken,
		Environment:       string(cfg.Environment),
		EncryptionKey:     cfg.EncryptionKey,
		AutoLoginUsername: cfg.AutoLoginUsername,
		AdminStaticAPIKey: cfg.AdminStaticAPIKey,
	}

	for _, envName := range cfg.DeprecatedEnvVarsSet() {
		slog.WarnContext(appCtx, "Deprecated environment variable is set and no longer used; remove it from your environment", "env", envName)
	}

	startup.LoadAgentToken(appCtx, runtimeCfg, p.Settings.GetStringSetting)
	startup.EnsureEncryptionKey(appCtx, runtimeCfg, p.Settings.EnsureEncryptionKey)
	cfg.AgentToken = runtimeCfg.AgentToken
	cfg.EncryptionKey = runtimeCfg.EncryptionKey

	if isWeakProductionEncryptionKey(cfg.EncryptionKey, string(cfg.Environment), cfg.AgentMode) {
		panic("ENCRYPTION_KEY passphrase must be at least 32 characters in production (or use a hex:/base64: encoded 32-byte key)")
	}

	crypto.InitEncryption(&crypto.Config{
		EncryptionKey: cfg.EncryptionKey,
		Environment:   string(cfg.Environment),
		AgentMode:     cfg.AgentMode,
	})
	startup.InitializeDefaultSettings(appCtx, runtimeCfg, p.Settings)

	// Startup repairs are best-effort: log each failure and continue.
	for _, task := range []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "normalize projects directory", run: func(ctx context.Context) error {
			return p.Settings.NormalizeProjectsDirectory(ctx, cfg.ProjectsDirectory)
		}},
		{name: "normalize builds directory", run: p.Settings.NormalizeBuildsDirectory},
		{name: "ensure local environment", run: func(ctx context.Context) error { return p.Environment.EnsureLocalEnvironment(ctx, cfg.AppUrl) }},
		{name: "clean up orphaned GitOps syncs", run: p.GitOpsSync.CleanupOrphanedSyncsOnStartup},
		{name: "clean up leaked GitOps scratch directories", run: p.GitOpsSync.CleanupLeakedScratchDirsOnStartup},
		{name: "clean up leaked git clone directories", run: p.GitOpsSync.CleanupLeakedCloneDirsOnStartup},
		{name: "reconcile directory GitOps projects", run: p.GitOpsSync.ReconcileDirectorySyncProjectsOnStartup},
		{name: "recover interrupted project renames", run: p.Project.RecoverProjectRenameJournals},
		{name: "recover interrupted container redeploys", run: p.Container.RecoverInterruptedRedeploys},
		{name: "recover stuck project statuses", run: p.Project.RecoverStuckProjectStatuses},
		{name: "fail interrupted image patches", run: p.Image.RecoverInterruptedPatches},
	} {
		if err := task.run(appCtx); err != nil {
			slog.WarnContext(appCtx, "Startup task failed", "task", task.name, "error", err)
		}
	}
	p.Vulnerability.ImportLegacyReportFiles(appCtx)
	p.Vulnerability.BackfillScanItems(appCtx)

	if !cfg.AgentMode {
		if err := p.Environment.ReconcileEdgeStatusesOnStartup(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to reconcile edge environment statuses on startup", "error", err)
		}
		p.Environment.RegisterMetrics()

		// Global variables are manager-owned: import a legacy local .env.global once, then sync everywhere.
		p.Environment.SetVariableSyncer(p.Variable)
		if err := p.Variable.ImportLegacyLocalEnvFile(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to import legacy global variables", "error", err)
		}
		go p.Variable.SyncAll(appCtx)
	}

	startup.TestDockerConnection(appCtx, func(ctx context.Context) error {
		_, err := p.Docker.Call(ctx, func(ctx context.Context, dockerClient *client.Client) (struct{}, error) {
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
	if err := p.Swarm.SyncSwarmEnabledState(appCtx); err != nil {
		slog.WarnContext(appCtx, "Failed to persist swarm enabled state", "error", err)
	}

	startup.InitializeNonAgentFeatures(appCtx, runtimeCfg,
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
			return p.ApiKey.ReconcileDefaultAdminAPIKey(ctx, runtimeCfg.AdminStaticAPIKey)
		},
		func(ctx context.Context) error {
			startup.InitializeAutoLogin(ctx, runtimeCfg)
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
		slog.ErrorContext(appCtx, "Failed to reconcile OIDC_ROLE_MAPPINGS", "error", err)
	}
	if !cfg.AgentMode {
		if err := p.Role.AssertGlobalAdminExists(appCtx, p.DB.WithContext(appCtx)); err != nil {
			slog.ErrorContext(appCtx, "RBAC global admin guard failed", "error", err)
		}
	}

	// Only edge agents auto-pair; direct agents are passive and the manager dials them.
	if cfg.AgentMode && cfg.EdgeAgent && cfg.AgentToken != "" && cfg.ManagerApiUrl != "" {
		if err := handleAgentBootstrapPairing(appCtx, cfg); err != nil {
			slog.WarnContext(appCtx, "Failed to auto-pair agent with manager", "error", err)
		}
	} else if cfg.AgentMode && !cfg.EdgeAgent {
		slog.InfoContext(appCtx, "Direct mode active: agent operates as a passive HTTP server; no outbound connection to manager required")
	}
}

func startEdgeTunnelClient(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, router *echo.Echo, _ *http.Server) {
	var stop func(context.Context) error
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if !cfg.EdgeAgent || cfg.ManagerApiUrl == "" || cfg.AgentToken == "" {
				return nil
			}
			tunnelStop, err := edge.StartTunnelClient(appCtx, buildEdgeRuntimeConfig(cfg), router)
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

func registerAppCancelHook(lc fx.Lifecycle, cancelApp context.CancelCauseFunc) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cancelApp(nil)
			return nil
		},
	})
}

func handleAgentBootstrapPairing(ctx context.Context, cfg *config.Config) error {
	slog.InfoContext(ctx, "Agent mode detected with token, attempting auto-pairing", "managerUrl", cfg.ManagerApiUrl)

	pairURL := strings.TrimRight(httpx.ManagerBaseURL(cfg.ManagerApiUrl), "/") + "/api/environments/pair"

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pairURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create pairing request: %w", err)
	}

	req.Header.Set("X-Api-Key", cfg.AgentToken)

	httpClient, err := edge.NewManagerHTTPClient(buildEdgeRuntimeConfig(cfg), 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to configure edge pairing client: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pairing request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

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
	return fmt.Errorf("pairing failed with status %d: %s", resp.StatusCode, string(body))
}
