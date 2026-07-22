package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"

	"github.com/moby/moby/client"
	"github.com/subosito/gotenv"

	"github.com/getarcaneapp/arcane/backend/v2/api/ws"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/di"
	"github.com/getarcaneapp/arcane/backend/v2/internal/services"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/startup"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	httputils "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	"github.com/labstack/echo/v5"
	"go.getarcane.app/streams/logs"
	libcrypto "go.getarcane.app/sys/crypto"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
)

func Bootstrap(ctx context.Context) error {
	if err := gotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.WrapIf(err, "load .env")
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
		return errors.WrapIf(err, "apply runtime identity")
	}
	cfg.DockerConfig = runtimeIdentityCfg.DockerConfig

	SetupSlogLogger(cfg)
	// Tee all slog output into the in-memory ring buffer that powers the
	// diagnostics live log tail.
	slog.SetDefault(slog.New(logs.NewSlogHandler(slog.Default().Handler(), ws.LogBroadcaster())))
	ConfigureGormLogger(cfg)
	slog.InfoContext(ctx, "Arcane is starting...", "version", config.Version)
	slog.InfoContext(ctx, "Arcane Identity Configuration", "PUID", os.Getuid(), "PGID", os.Getgid())

	appCtx, cancelApp := context.WithCancel(ctx)
	appCtx = utils.WithAppLifecycleContext(appCtx)

	db, err := initializeDBAndMigrate(appCtx, cfg)
	if err != nil {
		cancelApp()
		return errors.WrapIf(err, "failed to initialize database")
	}
	defer func() {
		cancelApp()
		if err := db.Close(); err != nil {
			slog.Error("Error closing database", "error", err)
		}
	}()

	app := fx.New(applicationOptions(appCtx, cfg, db, cancelApp))

	startCtx, cancelStart := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelStart()
	if err := app.Start(startCtx); err != nil {
		return errors.WrapIf(err, "start application")
	}

	select {
	case <-ctx.Done():
		slog.InfoContext(appCtx, "Context canceled")
	case signal := <-app.Done():
		slog.InfoContext(appCtx, "Received shutdown signal", "signal", signal)
	}

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelStop()
	if err := app.Stop(stopCtx); err != nil {
		return errors.WrapIf(err, "stop application")
	}

	slog.InfoContext(context.WithoutCancel(appCtx), "Arcane shutdown complete")
	return nil
}

func applicationOptions(appCtx context.Context, cfg *config.Config, db *database.DB, cancelApp context.CancelFunc) fx.Option {
	return fx.Options(
		fx.Supply(cfg, db, cancelApp),
		fx.Provide(
			func() context.Context { return appCtx },
			newConfiguredHTTPClient,
		),
		di.ServiceOptions,
		di.JobOptions,
		serverOptions,
		fx.Invoke(
			initializeStartupState,
			registerJobs,
			registerAppRollbackCancelHook,
			startEdgeTunnelClient,
			registerAppCancelHook,
		),
		fx.WithLogger(func() fxevent.Logger {
			logger := &fxevent.SlogLogger{Logger: slog.Default()}
			logger.UseLogLevel(slog.LevelDebug)
			return logger
		}),
		fx.StartTimeout(5*time.Minute),
		fx.StopTimeout(30*time.Second),
	)
}

// isWeakProductionEncryptionKeyInternal reports whether an explicit
// ENCRYPTION_KEY is an unprefixed passphrase shorter than 32 characters in
// production. libcrypto derives a key from any non-empty passphrase, so this
// preserves the historical fail-fast rejection of low-entropy production keys.
func isWeakProductionEncryptionKeyInternal(encryptionKey, environment string, agentMode bool) bool {
	if environment != "production" || agentMode {
		return false
	}
	key := strings.TrimSpace(encryptionKey)
	if key == "" || strings.HasPrefix(key, "hex:") || strings.HasPrefix(key, "base64:") {
		return false
	}
	return len(strings.TrimPrefix(key, "raw:")) < 32
}

func newConfiguredHTTPClient(cfg *config.Config) *http.Client {
	if cfg.HTTPClientTimeout > 0 {
		return httputils.NewHTTPClientWithTimeout(time.Duration(cfg.HTTPClientTimeout) * time.Second)
	}
	return httputils.NewHTTPClient()
}

type initializeStartupStateParams struct {
	fx.In

	AppCtx     context.Context
	Config     *config.Config
	HTTPClient *http.Client

	Volume      *services.VolumeService
	Settings    *services.SettingsService
	Environment *services.EnvironmentService
	GitOpsSync  *services.GitOpsSyncService
	Project     *services.ProjectService
	Variable    *services.VariableService
	Docker      *services.DockerClientService
	Swarm       *services.SwarmService
	Role        *services.RoleService
	User        *services.UserService
	ApiKey      *services.ApiKeyService
}

func initializeStartupState(p initializeStartupStateParams) {
	appCtx := p.AppCtx
	cfg := p.Config
	httpClient := p.HTTPClient

	if p.Volume != nil {
		startup.CleanupOrphanedVolumeHelpers(appCtx, p.Volume.CleanupOrphanedVolumeHelpers)
	}

	runtimeCfg := &startup.RuntimeConfig{
		AgentMode:         cfg.AgentMode,
		AgentToken:        cfg.AgentToken,
		Environment:       string(cfg.Environment),
		EncryptionKey:     cfg.EncryptionKey,
		AutoLoginUsername: cfg.AutoLoginUsername,
		AdminStaticAPIKey: cfg.AdminStaticAPIKey,
	}

	startup.LoadAgentToken(appCtx, runtimeCfg, p.Settings.GetStringSetting)
	startup.EnsureEncryptionKey(appCtx, runtimeCfg, p.Settings.EnsureEncryptionKey)
	cfg.AgentToken = runtimeCfg.AgentToken
	cfg.EncryptionKey = runtimeCfg.EncryptionKey

	if isWeakProductionEncryptionKeyInternal(cfg.EncryptionKey, string(cfg.Environment), cfg.AgentMode) {
		panic("ENCRYPTION_KEY passphrase must be at least 32 characters in production (or use a hex:/base64: encoded 32-byte key)")
	}

	libcrypto.InitEncryption(&libcrypto.Config{
		EncryptionKey: cfg.EncryptionKey,
		Environment:   string(cfg.Environment),
		AgentMode:     cfg.AgentMode,
	})
	startup.InitializeDefaultSettings(appCtx, runtimeCfg, p.Settings)

	if err := p.Settings.NormalizeProjectsDirectory(appCtx, cfg.ProjectsDirectory); err != nil {
		slog.WarnContext(appCtx, "Failed to normalize projects directory", "error", err)
	}

	if err := p.Settings.NormalizeBuildsDirectory(appCtx); err != nil {
		slog.WarnContext(appCtx, "Failed to normalize builds directory", "error", err)
	}

	if err := p.Environment.EnsureLocalEnvironment(appCtx, cfg.AppUrl); err != nil {
		slog.WarnContext(appCtx, "Failed to ensure local environment", "error", err)
	}
	initializeGitOpsStartupStateInternal(appCtx, p.GitOpsSync)
	if p.Project != nil {
		if err := p.Project.RecoverProjectRenameJournals(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to recover interrupted project rename operations on startup", "error", err)
		}
	}

	if !cfg.AgentMode {
		if err := p.Environment.ReconcileEdgeStatusesOnStartup(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to reconcile edge environment statuses on startup", "error", err)
		}

		// Global variables are a manager resource: import any pre-existing local
		// .env.global once, then materialize the effective set everywhere. Agents
		// only serve the per-environment variables endpoint the manager pushes to.
		p.Environment.SetVariableSyncer(p.Variable)
		if err := p.Variable.ImportLegacyLocalEnvFile(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to import legacy global variables", "error", err)
		}
		go p.Variable.SyncAll(appCtx)
	}

	startup.TestDockerConnection(appCtx, func(ctx context.Context) error {
		dockerClient, err := p.Docker.GetClient(ctx)
		if err != nil {
			return err
		}

		version, err := dockerClient.ServerVersion(ctx, client.ServerVersionOptions{})
		if err != nil {
			return err
		}

		effectiveAPIVersion := strings.TrimSpace(dockerClient.ClientVersion())
		if effectiveAPIVersion == "" {
			effectiveAPIVersion = strings.TrimSpace(version.APIVersion)
		}
		slog.InfoContext(ctx, "Docker API versions detected", "client_api_version", dockerClient.ClientVersion(), "server_api_version", version.APIVersion, "effective_api_version", effectiveAPIVersion)
		return nil
	})
	if p.Swarm != nil {
		if err := p.Swarm.SyncSwarmEnabledState(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to persist swarm enabled state", "error", err)
		}
	}

	startup.InitializeNonAgentFeatures(appCtx, runtimeCfg,
		p.Role.EnsureBuiltInRoles,
		p.User.CreateDefaultAdmin,
		func(ctx context.Context) error {
			return p.ApiKey.ReconcileDefaultAdminAPIKey(ctx, runtimeCfg.AdminStaticAPIKey)
		},
		func(ctx context.Context) error {
			startup.InitializeAutoLogin(ctx, runtimeCfg)
			return nil
		},
	)
	startup.CleanupUnknownSettings(appCtx, p.Settings)

	runRoleStartupTasks(appCtx, p.Role, cfg, cfg.AgentMode)

	// Auto-pair only applies in Edge mode (where the agent's outbound tunnel is the
	// only path to the manager). Direct mode is passive — the manager dials the agent's
	// HTTP server on TCP 3553, and the manager-side health-check promotes the env to
	// Online once reachability is confirmed.
	if cfg.AgentMode && cfg.EdgeAgent && cfg.AgentToken != "" && cfg.ManagerApiUrl != "" {
		if err := handleAgentBootstrapPairing(appCtx, cfg, httpClient); err != nil {
			slog.WarnContext(appCtx, "Failed to auto-pair agent with manager", "error", err)
		}
	} else if cfg.AgentMode && !cfg.EdgeAgent {
		slog.InfoContext(appCtx, "Direct mode active: agent operates as a passive HTTP server; no outbound connection to manager required")
	}
}

func initializeGitOpsStartupStateInternal(appCtx context.Context, gitOpsSync *services.GitOpsSyncService) {
	if gitOpsSync == nil {
		return
	}
	if err := gitOpsSync.CleanupOrphanedSyncsOnStartup(appCtx); err != nil {
		slog.WarnContext(appCtx, "Failed to clean up orphaned GitOps syncs on startup", "error", err)
	}
	// Sweep leaked gitops scratch dirs before the filesystem watcher can import
	// them as phantom projects and before the directory-sync reconcile runs.
	if err := gitOpsSync.CleanupLeakedScratchDirsOnStartup(appCtx); err != nil {
		slog.WarnContext(appCtx, "Failed to clean up leaked GitOps scratch directories on startup", "error", err)
	}
	if err := gitOpsSync.ReconcileDirectorySyncProjectsOnStartup(appCtx); err != nil {
		slog.WarnContext(appCtx, "Failed to reconcile directory GitOps projects on startup", "error", err)
	}
}

func runRoleStartupTasks(ctx context.Context, roleService *services.RoleService, cfg *config.Config, agentMode bool) {
	if roleService == nil {
		return
	}
	if err := roleService.EnsureBuiltInRoles(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to reconcile built-in roles", "error", err)
	}
	// Backfill must run AFTER EnsureBuiltInRoles (it references the role IDs
	// seeded there) and BEFORE BackfillApiKeyPermissions / AssertGlobalAdminExists
	// (both consult the assignments table this populates).
	if err := roleService.BackfillLegacyRoleAssignments(ctx); err != nil {
		slog.ErrorContext(ctx, "Failed to backfill legacy users.roles into user_role_assignments", "error", err)
	}
	if err := roleService.BackfillApiKeyPermissions(ctx); err != nil {
		slog.WarnContext(ctx, "Failed to backfill API key permissions", "error", err)
	}
	if cfg != nil {
		if err := roleService.ReconcileEnvOidcMappings(ctx, cfg.OidcRoleMappings); err != nil {
			slog.ErrorContext(ctx, "Failed to reconcile OIDC_ROLE_MAPPINGS", "error", err)
		}
	}
	if agentMode {
		return
	}
	if err := roleService.AssertGlobalAdminExists(ctx); err != nil {
		slog.ErrorContext(ctx, "RBAC global admin guard failed", "error", err)
	}
}

func startEdgeTunnelClient(appCtx context.Context, lc fx.Lifecycle, cfg *config.Config, router *echo.Echo, _ *http.Server) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			startEdgeTunnelClientIfConfigured(appCtx, cfg, router)
			return nil
		},
	})
}

func registerAppRollbackCancelHook(lc fx.Lifecycle, cancelApp context.CancelFunc) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cancelApp()
			return nil
		},
	})
}

func registerAppCancelHook(lc fx.Lifecycle, cancelApp context.CancelFunc) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cancelApp()
			return nil
		},
	})
}

func startEdgeTunnelClientIfConfigured(appCtx context.Context, cfg *config.Config, router http.Handler) {
	managerEndpointConfigured := cfg.ManagerApiUrl != ""
	if !cfg.EdgeAgent || !managerEndpointConfigured || cfg.AgentToken == "" {
		return
	}

	edgeCfg := &edge.Config{
		EdgeAgent:             cfg.EdgeAgent,
		EdgeTransport:         cfg.EdgeTransport,
		EdgeReconnectInterval: cfg.EdgeReconnectInterval,
		EdgeMTLSMode:          cfg.EdgeMTLSMode,
		EdgeMTLSCAFile:        cfg.EdgeMTLSCAFile,
		EdgeMTLSCertFile:      cfg.EdgeMTLSCertFile,
		EdgeMTLSKeyFile:       cfg.EdgeMTLSKeyFile,
		EdgeMTLSServerName:    cfg.EdgeMTLSServerName,
		EdgeMTLSAssetsDir:     cfg.EdgeMTLSAssetsDir,
		AppURL:                cfg.GetAppURL(),
		ManagerApiUrl:         cfg.ManagerApiUrl,
		AgentToken:            cfg.AgentToken,
		Port:                  cfg.Port,
		Listen:                cfg.Listen,
	}

	slog.InfoContext(appCtx, "Starting edge agent session client", edge.StartupLogAttrs(edgeCfg)...)
	errCh, err := edge.StartTunnelClientWithErrors(appCtx, edgeCfg, router)
	if err != nil {
		slog.ErrorContext(appCtx, "Failed to start edge tunnel client", "error", err)
		return
	}

	slog.InfoContext(appCtx, "Edge tunnel client started", "manager_url", cfg.ManagerApiUrl)
	go func() {
		for err := range errCh {
			slog.ErrorContext(appCtx, "Edge tunnel client error", "error", err)
		}
	}()
}

func handleAgentBootstrapPairing(ctx context.Context, cfg *config.Config, httpClient *http.Client) error {
	slog.InfoContext(ctx, "Agent mode detected with token, attempting auto-pairing", "managerUrl", cfg.ManagerApiUrl)

	pairURL := strings.TrimRight(cfg.GetManagerBaseURL(), "/") + "/api/environments/pair"

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pairURL, nil)
	if err != nil {
		return errors.WrapIf(err, "failed to create pairing request")
	}

	req.Header.Set("X-Api-Key", cfg.AgentToken)

	if cfg.EdgeAgent && strings.TrimSpace(cfg.ManagerApiUrl) != "" {
		edgeClient, edgeErr := edge.NewManagerHTTPClient(&edge.Config{
			ManagerApiUrl:      cfg.ManagerApiUrl,
			EdgeMTLSMode:       cfg.EdgeMTLSMode,
			EdgeMTLSCAFile:     cfg.EdgeMTLSCAFile,
			EdgeMTLSCertFile:   cfg.EdgeMTLSCertFile,
			EdgeMTLSKeyFile:    cfg.EdgeMTLSKeyFile,
			EdgeMTLSServerName: cfg.EdgeMTLSServerName,
			EdgeMTLSAssetsDir:  cfg.EdgeMTLSAssetsDir,
		}, 10*time.Second)
		if edgeErr != nil {
			return errors.WrapIf(edgeErr, "failed to configure edge pairing client")
		}
		httpClient = edgeClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.WrapIf(err, "pairing request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		slog.InfoContext(ctx, "Successfully paired agent with manager", "managerUrl", cfg.ManagerApiUrl)
		return nil
	case http.StatusBadRequest:
		// Environment is not in pending status - already paired, this is fine
		if strings.Contains(string(body), "not in pending status") {
			slog.InfoContext(ctx, "Agent already paired with manager", "managerUrl", cfg.ManagerApiUrl)
			return nil
		}
		return errors.Errorf("pairing failed with status %d: %s", resp.StatusCode, string(body))
	case http.StatusUnauthorized:
		// Invalid API key - could be already paired with a different key, or key was deleted
		// This is not fatal; the agent can still function if it has a valid token configured
		slog.DebugContext(ctx, "Pairing skipped - API key not recognized (agent may already be paired)", "managerUrl", cfg.ManagerApiUrl)
		return nil
	default:
		return errors.Errorf("pairing failed with status %d: %s", resp.StatusCode, string(body))
	}
}
