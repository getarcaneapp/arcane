package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	"github.com/getarcaneapp/arcane/backend/internal/utils/edge"
	httputils "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	"github.com/getarcaneapp/arcane/backend/pkg/scheduler"
)

func Bootstrap(ctx context.Context) error {
	_ = godotenv.Load()
	cfg := config.Load()

	SetupGinLogger(cfg)
	ConfigureGormLogger(cfg)
	slog.InfoContext(ctx, "Arcane is starting", "version", config.Version)

	appCtx, cancelApp := context.WithCancel(ctx)
	defer cancelApp()

	db, err := initializeDBAndMigrate(appCtx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	defer func(ctx context.Context) {
		// Use background context for shutdown as appCtx is already canceled
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second) //nolint:contextcheck
		defer shutdownCancel()
		if err := db.Close(); err != nil {
			slog.ErrorContext(shutdownCtx, "Error closing database", "error", err) //nolint:contextcheck
		}
	}(appCtx)

	httpClient := httputils.NewHTTPClient()
	if cfg.HTTPClientTimeout > 0 {
		httpClient = httputils.NewHTTPClientWithTimeout(time.Duration(cfg.HTTPClientTimeout) * time.Second)
	}

	appServices, dockerClientService, err := initializeServices(appCtx, db, cfg, httpClient)
	if err != nil {
		return fmt.Errorf("failed to initialize services: %w", err)
	}
	defer func(ctx context.Context) {
		baseCtx := context.WithoutCancel(ctx)
		shutdownCtx, shutdownCancel := context.WithTimeout(baseCtx, 10*time.Second)
		defer shutdownCancel()
		if appServices.Volume != nil {
			appServices.Volume.CleanupHelperContainers(shutdownCtx)
		}
	}(appCtx)

	if appServices.Volume != nil {
		if err := appServices.Volume.CleanupOrphanedVolumeHelpers(appCtx); err != nil {
			slog.WarnContext(appCtx, "Failed to cleanup orphaned volume helpers on startup", "error", err)
		}
	}

	utils.LoadAgentToken(appCtx, cfg, appServices.Settings.GetStringSetting)
	utils.EnsureEncryptionKey(appCtx, cfg, appServices.Settings.EnsureEncryptionKey)
	crypto.InitEncryption(cfg)
	utils.InitializeDefaultSettings(appCtx, cfg, appServices.Settings)
	utils.MigrateSchedulerCronValues(
		appCtx,
		appServices.Settings.GetStringSetting,
		appServices.Settings.UpdateSetting,
		appServices.Settings.LoadDatabaseSettings,
	)
	if appServices.GitOpsSync != nil {
		utils.MigrateGitOpsSyncIntervals(
			appCtx,
			appServices.GitOpsSync.ListSyncIntervalsRaw,
			appServices.GitOpsSync.UpdateSyncIntervalMinutes,
		)
	}

	if err := appServices.Settings.NormalizeProjectsDirectory(appCtx, cfg.ProjectsDirectory); err != nil {
		slog.WarnContext(appCtx, "Failed to normalize projects directory", "error", err)
	}

	if err := appServices.Environment.EnsureLocalEnvironment(appCtx, cfg.AppUrl); err != nil {
		slog.WarnContext(appCtx, "Failed to ensure local environment", "error", err)
	}

	utils.TestDockerConnection(appCtx, func(ctx context.Context) error {
		dockerClient, err := dockerClientService.GetClient()
		if err != nil {
			return err
		}
		_, err = dockerClient.Ping(ctx)
		return err
	})

	utils.InitializeNonAgentFeatures(appCtx, cfg,
		appServices.User.CreateDefaultAdmin,
		func(ctx context.Context) error {
			utils.InitializeAutoLogin(ctx, cfg)
			return nil
		},
		appServices.Settings.MigrateOidcConfigToFields,
		appServices.Notification.MigrateDiscordWebhookUrlToFields)
	utils.CleanupUnknownSettings(appCtx, appServices.Settings)

	// Handle agent auto-pairing with API key
	if cfg.AgentMode && cfg.AgentToken != "" && cfg.ManagerApiUrl != "" {
		if err := handleAgentBootstrapPairing(appCtx, cfg, httpClient); err != nil {
			slog.WarnContext(appCtx, "Failed to auto-pair agent with manager", "error", err)
		}
	}

	scheduler := scheduler.NewJobScheduler(appCtx)
	appServices.JobSchedule.SetScheduler(scheduler)
	registerJobs(appCtx, scheduler, appServices, cfg)

	router, tunnelServer := setupRouter(appCtx, cfg, appServices)

	// Start edge tunnel client if running as an edge agent
	if cfg.EdgeAgent && cfg.ManagerApiUrl != "" && cfg.AgentToken != "" {
		slog.InfoContext(appCtx, "Starting edge tunnel client", "manager_url", cfg.ManagerApiUrl)
		errCh, err := edge.StartTunnelClientWithErrors(appCtx, cfg, router)
		if err != nil {
			slog.ErrorContext(appCtx, "Failed to start edge tunnel client", "error", err)
		} else {
			slog.InfoContext(appCtx, "Edge tunnel client started", "manager_url", cfg.ManagerApiUrl)
			go func() {
				for err := range errCh {
					slog.ErrorContext(appCtx, "Edge tunnel client error", "error", err)
				}
			}()
		}
	}

	err = runServices(appCtx, cfg, router, tunnelServer, scheduler)
	if err != nil {
		return fmt.Errorf("failed to run services: %w", err)
	}

	slog.InfoContext(appCtx, "Arcane shutdown complete")
	return nil
}

func handleAgentBootstrapPairing(ctx context.Context, cfg *config.Config, httpClient *http.Client) error {
	slog.InfoContext(ctx, "Agent mode detected with token, attempting auto-pairing", "managerUrl", cfg.ManagerApiUrl)

	pairURL := strings.TrimRight(cfg.GetManagerBaseURL(), "/") + "/api/environments/pair"

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pairURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create pairing request: %w", err)
	}

	req.Header.Set("X-API-Key", cfg.AgentToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pairing request failed: %w", err)
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
		return fmt.Errorf("pairing failed with status %d: %s", resp.StatusCode, string(body))
	case http.StatusUnauthorized:
		// Invalid API key - could be already paired with a different key, or key was deleted
		// This is not fatal; the agent can still function if it has a valid token configured
		slog.DebugContext(ctx, "Pairing skipped - API key not recognized (agent may already be paired)", "managerUrl", cfg.ManagerApiUrl)
		return nil
	default:
		return fmt.Errorf("pairing failed with status %d: %s", resp.StatusCode, string(body))
	}
}

func runServices(appCtx context.Context, cfg *config.Config, router http.Handler, tunnelServer *edge.TunnelServer, schedulers ...interface{ Run(context.Context) error }) error {
	for _, s := range schedulers {
		scheduler := s
		go func() {
			slog.InfoContext(appCtx, "Starting scheduler")
			if err := scheduler.Run(appCtx); err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.ErrorContext(appCtx, "Job scheduler exited with error", "error", err)
				}
			}
			slog.InfoContext(appCtx, "Scheduler stopped")
		}()
	}

	listenAddr := cfg.ListenAddr()
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.InfoContext(appCtx, "Starting HTTP server", "addr", listenAddr, "listen", cfg.Listen, "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.ErrorContext(appCtx, "Failed to start server", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		slog.InfoContext(appCtx, "Received shutdown signal")
	case <-appCtx.Done():
		slog.InfoContext(appCtx, "Context canceled")
	}

	// Use background context for shutdown as appCtx is already canceled
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second) //nolint:contextcheck
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck
		slog.ErrorContext(shutdownCtx, "Server forced to shutdown", "error", err) //nolint:contextcheck
		return err
	}

	// Wait for tunnel cleanup loop to finish
	if tunnelServer != nil {
		tunnelServer.WaitForCleanupDone()
	}

	slog.InfoContext(shutdownCtx, "Server stopped gracefully") //nolint:contextcheck
	return nil
}
