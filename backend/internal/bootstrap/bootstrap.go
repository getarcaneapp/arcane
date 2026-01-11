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
	httputils "github.com/getarcaneapp/arcane/backend/internal/utils/http"
)

func Bootstrap(ctx context.Context) error {
	_ = godotenv.Load()
	cfg := config.Load()

	SetupGinLogger(cfg)
	ConfigureGormLogger(cfg)
	slog.InfoContext(ctx, "Arcane is starting", "version", config.Version)

	appCtx, cancelApp := context.WithCancel(ctx)
	defer cancelApp()

	db, err := initializeDBAndMigrate(cfg)
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

	appServices, dockerClientService, err := initializeServices(appCtx, db, cfg, httpClient)
	if err != nil {
		return fmt.Errorf("failed to initialize services: %w", err)
	}

	utils.LoadAgentToken(appCtx, cfg, appServices.Settings.GetStringSetting)
	utils.EnsureEncryptionKey(appCtx, cfg, appServices.Settings.EnsureEncryptionKey)
	crypto.InitEncryption(cfg)
	utils.InitializeDefaultSettings(appCtx, cfg, appServices.Settings)

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
		appServices.Settings.MigrateOidcConfigToFields)

	// Handle agent auto-pairing with API key
	if cfg.AgentMode && cfg.AgentToken != "" && cfg.ManagerApiUrl != "" {
		if err := handleAgentBootstrapPairing(appCtx, cfg, httpClient); err != nil {
			slog.WarnContext(appCtx, "Failed to auto-pair agent with manager", "error", err)
		}
	}

	scheduler, err := initializeScheduler()
	if err != nil {
		return fmt.Errorf("failed to create job scheduler: %w", err)
	}
	registerJobs(appCtx, scheduler, appServices, cfg)

	router := setupRouter(cfg, appServices) //nolint:contextcheck

	err = runServices(appCtx, cfg, router, scheduler)
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pairing failed with status %d: %s", resp.StatusCode, string(body))
	}

	slog.InfoContext(ctx, "Successfully paired agent with manager", "managerUrl", cfg.ManagerApiUrl)

	return nil
}

func runServices(appCtx context.Context, cfg *config.Config, router http.Handler, scheduler interface{ Run(context.Context) error }) error {
	go func() {
		slog.InfoContext(appCtx, "Starting scheduler")
		if err := scheduler.Run(appCtx); err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.ErrorContext(appCtx, "Job scheduler exited with error", "error", err)
			}
		}
		slog.InfoContext(appCtx, "Scheduler stopped")
	}()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.InfoContext(appCtx, "Starting HTTP server", "port", cfg.Port)
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

	slog.InfoContext(shutdownCtx, "Server stopped gracefully") //nolint:contextcheck
	return nil
}
