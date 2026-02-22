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
	"github.com/getarcaneapp/arcane/backend/internal/services"
	"github.com/getarcaneapp/arcane/backend/internal/utils"
	"github.com/getarcaneapp/arcane/backend/internal/utils/crypto"
	httputils "github.com/getarcaneapp/arcane/backend/internal/utils/http"
	"github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge"
	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/getarcaneapp/arcane/backend/pkg/scheduler"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
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

	httpClient := newConfiguredHTTPClient(cfg)

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

	initializeStartupState(appCtx, cfg, appServices, dockerClientService, httpClient)

	cronLocation := cfg.GetLocation()
	scheduler := scheduler.NewJobScheduler(appCtx, cronLocation)
	appServices.JobSchedule.SetScheduler(scheduler)
	registerJobs(appCtx, scheduler, appServices, cfg)

	router, tunnelServer := setupRouter(appCtx, cfg, appServices)

	startEdgeTunnelClientIfConfigured(appCtx, cfg, router)

	err = runServices(appCtx, cfg, router, tunnelServer, scheduler)
	if err != nil {
		return fmt.Errorf("failed to run services: %w", err)
	}

	slog.InfoContext(appCtx, "Arcane shutdown complete")
	return nil
}

func newConfiguredHTTPClient(cfg *config.Config) *http.Client {
	if cfg.HTTPClientTimeout > 0 {
		return httputils.NewHTTPClientWithTimeout(time.Duration(cfg.HTTPClientTimeout) * time.Second)
	}
	return httputils.NewHTTPClient()
}

func initializeStartupState(appCtx context.Context, cfg *config.Config, appServices *Services, dockerClientService *services.DockerClientService, httpClient *http.Client) {
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

		// Force version negotiation before logging effective client API version.
		dockerClient.NegotiateAPIVersion(ctx)

		version, err := dockerClient.ServerVersion(ctx)
		if err != nil {
			return err
		}

		slog.InfoContext(ctx, "Docker API versions detected", "client_api_version", dockerClient.ClientVersion(), "server_api_version", version.APIVersion)
		return nil
	})

	utils.InitializeNonAgentFeatures(appCtx, cfg,
		appServices.User.CreateDefaultAdmin,
		func(ctx context.Context) error {
			utils.InitializeAutoLogin(ctx, cfg)
			return nil
		},
		appServices.Settings.MigrateOidcConfigToFields,
		appServices.Notification.MigrateDiscordWebhookUrlToFields,
	)
	utils.CleanupUnknownSettings(appCtx, appServices.Settings)

	// Handle agent auto-pairing with API key.
	if cfg.AgentMode && cfg.AgentToken != "" && cfg.ManagerApiUrl != "" {
		if err := handleAgentBootstrapPairing(appCtx, cfg, httpClient); err != nil {
			slog.WarnContext(appCtx, "Failed to auto-pair agent with manager", "error", err)
		}
	}
}

func startEdgeTunnelClientIfConfigured(appCtx context.Context, cfg *config.Config, router http.Handler) {
	managerEndpointConfigured := cfg.ManagerApiUrl != ""
	if !cfg.EdgeAgent || !managerEndpointConfigured || cfg.AgentToken == "" {
		return
	}

	slog.InfoContext(appCtx, "Starting edge tunnel client",
		"transport_mode", edge.NormalizeEdgeTransport(cfg.EdgeTransport),
		"attempt_grpc", edge.UseGRPCEdgeTransport(cfg),
		"attempt_websocket", edge.UseWebSocketEdgeTransport(cfg),
		"manager_url", cfg.ManagerApiUrl,
	)
	errCh, err := edge.StartTunnelClientWithErrors(appCtx, cfg, router)
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
		return fmt.Errorf("failed to create pairing request: %w", err)
	}

	req.Header.Set("X-API-Key", cfg.AgentToken)

	resp, err := httpClient.Do(req) //nolint:gosec // intentional request to configured manager pairing endpoint
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
	httpHandler := router

	var grpcServer *grpc.Server
	if !cfg.AgentMode && tunnelServer != nil {
		grpcServer = grpc.NewServer(tunnelServer.GRPCServerOptions(appCtx)...)
		tunnelpb.RegisterTunnelServiceServer(grpcServer, tunnelServer)

		httpHandler = h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isTunnelGRPCRequestInternal(r) {
				grpcReq := normalizeTunnelGRPCRequestPathInternal(r)
				grpcServer.ServeHTTP(w, grpcReq)
				return
			}
			router.ServeHTTP(w, r)
		}), &http2.Server{})
		slog.InfoContext(appCtx, "Using shared HTTP/gRPC listener for edge tunnel", "addr", listenAddr)
	}

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           httpHandler,
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

	if grpcServer != nil {
		grpcServer.GracefulStop()
	}

	// Wait for tunnel cleanup loop to finish
	if tunnelServer != nil {
		tunnelServer.WaitForCleanupDone()
	}

	slog.InfoContext(shutdownCtx, "Server stopped gracefully") //nolint:contextcheck
	return nil
}

func normalizeTunnelGRPCRequestPathInternal(r *http.Request) *http.Request {
	if r == nil {
		return nil
	}
	if r.URL == nil {
		return r
	}

	connectMethodPath := tunnelpb.TunnelService_Connect_FullMethodName
	legacyAPIPath := "/api/tunnel/connect"
	if strings.HasSuffix(r.URL.Path, legacyAPIPath) {
		clone := r.Clone(r.Context())
		cloneURL := *clone.URL
		cloneURL.Path = connectMethodPath
		clone.URL = &cloneURL
		clone.RequestURI = connectMethodPath
		return clone
	}

	idx := strings.Index(r.URL.Path, connectMethodPath)
	if idx <= 0 {
		return r
	}

	normalizedPath := r.URL.Path[idx:]
	if normalizedPath == r.URL.Path {
		return r
	}

	clone := r.Clone(r.Context())
	cloneURL := *clone.URL
	cloneURL.Path = normalizedPath
	clone.URL = &cloneURL
	clone.RequestURI = normalizedPath
	return clone
}

func isTunnelGRPCRequestInternal(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}

	if r.Method != http.MethodPost {
		return false
	}

	path := r.URL.Path
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName
	if path == fullMethodPath || strings.HasSuffix(path, fullMethodPath) || strings.HasSuffix(path, "/api/tunnel/connect") {
		return true
	}

	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "application/grpc") ||
		strings.HasPrefix(contentType, "application/grpc+") ||
		strings.HasPrefix(contentType, "application/grpc-web") {
		return true
	}

	te := strings.ToLower(strings.TrimSpace(r.Header.Get("Te")))
	return r.ProtoMajor >= 2 && strings.Contains(te, "trailers")
}
