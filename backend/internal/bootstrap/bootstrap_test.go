package bootstrap

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	schedulertypes "github.com/getarcaneapp/arcane/types/v2/scheduler"
	"github.com/labstack/echo/v5"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	libcrypto "go.getarcane.app/sys/crypto"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/auth"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/gitops"
	"github.com/getarcaneapp/arcane/backend/v2/internal/job"
	"github.com/getarcaneapp/arcane/backend/v2/internal/kv"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/swarm"
	"github.com/getarcaneapp/arcane/backend/v2/internal/system"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/flow"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/scheduler/runs"
	francistest "github.com/getarcaneapp/arcane/backend/v2/pkg/utils/francis/testing"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils/httpx"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/proto/tunnel/v1"
)

type blockingBusWatcher struct {
	started chan struct{}
	stopped chan struct{}
}

func (w *blockingBusWatcher) Name() string { return "blocking" }

func (w *blockingBusWatcher) Start(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	close(w.stopped)
	return nil
}

func (w *blockingBusWatcher) RunNow(context.Context) error { return nil }

func TestNormalizeTunnelGRPCRequestPath(t *testing.T) {
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	t.Run("nil request", func(t *testing.T) {
		assert.Nil(t, normalizeTunnelGRPCRequestPathInternal(nil))
	})

	t.Run("path without prefix remains unchanged", func(t *testing.T) {
		req := httptest.NewRequest("POST", fullMethodPath, http.NoBody)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.Same(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
	})

	t.Run("api prefix is removed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api"+fullMethodPath, http.NoBody)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("nested proxy prefix is removed up to method path", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/edge/proxy/api"+fullMethodPath, http.NoBody)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("legacy /api/tunnel/connect is rewritten to gRPC method", func(t *testing.T) {
		// Edge agents use /api/tunnel/connect as their gRPC method path so reverse proxies
		// can route tunnel traffic on a stable URL.
		req := httptest.NewRequest("POST", "/api/tunnel/connect", http.NoBody)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("nested proxy with legacy /api/tunnel/connect is rewritten", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/edge/proxy/api/tunnel/connect", http.NoBody)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})
}

func TestIsTunnelGRPCRequest(t *testing.T) {
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	t.Run("detects by grpc content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/any/path", http.NoBody)
		req.Header.Set("Content-Type", "application/grpc")
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by grpc-web content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/any/path", http.NoBody)
		req.Header.Set("Content-Type", "application/grpc-web+proto")
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by method path without grpc content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fullMethodPath, http.NoBody)
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match regular api requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/environments/pair", http.NoBody)
		req.Header.Set("Content-Type", "application/json")
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("requires post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fullMethodPath, http.NoBody)
		req.Header.Set("Content-Type", "application/grpc")
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match http2 post with te trailers and json content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", http.NoBody)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Te", "trailers")
		req.ProtoMajor = 2
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match http2 post with te trailers and form content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/logout", http.NoBody)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Te", "trailers")
		req.ProtoMajor = 2
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})
}

func TestConfigureHTTPProtocols(t *testing.T) {
	handler := http.NewServeMux()

	t.Run("tls enables http1 and http2", func(t *testing.T) {
		configuredHandler, protocols := configureHTTPProtocolsInternal(true, handler)

		assert.Same(t, handler, configuredHandler)
		require.NotNil(t, protocols)
		assert.True(t, protocols.HTTP1())
		assert.True(t, protocols.HTTP2())
		assert.False(t, protocols.UnencryptedHTTP2())
	})

	t.Run("plain enables http1 and unencrypted http2", func(t *testing.T) {
		configuredHandler, protocols := configureHTTPProtocolsInternal(false, handler)

		// The plain path wraps the handler to clear the stale h2c read
		// deadline, so it is deliberately not the same handler.
		assert.NotNil(t, configuredHandler)
		require.NotNil(t, protocols)
		assert.True(t, protocols.HTTP1())
		assert.False(t, protocols.HTTP2())
		assert.True(t, protocols.UnencryptedHTTP2())
	})
}

func TestHTTP2APIResponsesDoNotUseAPIGzip(t *testing.T) {
	cfg := &config.Config{
		AgentMode:   true,
		AppUrl:      "http://localhost:3552",
		Environment: config.AppEnvironmentTest,
	}
	router, _ := newRouter(RouterParams{
		Context: t.Context(),
		Config:  cfg,
		HandlerDeps: api.HandlerDeps{
			Project:   project.New(&project.ProjectService{}, nil),
			Container: container.New(&container.ContainerService{}, nil, nil, nil),
			Swarm:     swarm.New(&swarm.SwarmService{}, nil, nil, nil),
			System:    system.New(&system.SystemService{}, nil, nil, nil),
		},
		AuthMiddleware: auth.NewAuthMiddleware(nil, cfg),
		TunnelRegistry: edge.NewTunnelRegistry(),
	})
	handler, protocols := configureHTTPProtocolsInternal(false, router)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		require.NoError(t, server.Shutdown(context.WithoutCancel(t.Context())))
		require.ErrorIs(t, <-errCh, http.ErrServerClosed)
	})

	clientProtocols := &http.Protocols{}
	clientProtocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: clientProtocols, DisableCompression: true}
	client := &http.Client{Transport: transport}

	for _, path := range []string{"/api/health", "/api/openapi.json"} {
		t.Run(path, func(t *testing.T) {
			req, requestErr := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String()+path, http.NoBody)
			require.NoError(t, requestErr)
			req.Header.Set("Accept-Encoding", "gzip")

			resp, responseErr := client.Do(req)
			require.NoError(t, responseErr)
			defer func() { _ = resp.Body.Close() }()

			body, readErr := io.ReadAll(resp.Body)
			require.NoError(t, readErr)

			require.Equal(t, "HTTP/2.0", resp.Proto)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			require.Empty(t, resp.Header.Get("Content-Encoding"))

			if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
				parsedLength, parseErr := strconv.Atoi(contentLength)
				require.NoError(t, parseErr)
				require.Equal(t, len(body), parsedLength)
			}
		})
	}
}

func TestH2CStreamSurvivesPastReadHeaderTimeout(t *testing.T) {
	// go1.26.6 arms ReadHeaderTimeout before the h2c preface sniff and never clears it, so
	// without ClearReadDeadline every h2c stream dies ReadHeaderTimeout after accept.
	const readHeaderTimeout = 250 * time.Millisecond

	release := make(chan struct{})
	streamHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
		_, _ = w.Write([]byte("done"))
	})
	handler, protocols := configureHTTPProtocolsInternal(false, streamHandler)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: readHeaderTimeout,
		ConnContext:       httpx.WithConn,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		require.NoError(t, server.Shutdown(context.WithoutCancel(t.Context())))
		require.ErrorIs(t, <-errCh, http.ErrServerClosed)
	})

	clientProtocols := &http.Protocols{}
	clientProtocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: clientProtocols}
	client := &http.Client{Transport: transport}

	resp, err := client.Get("http://" + listener.Addr().String() + "/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, "HTTP/2.0", resp.Proto)

	// Idle past ReadHeaderTimeout; a stale deadline would fail the read below.
	time.Sleep(4 * readHeaderTimeout)
	close(release)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "done", string(body))
}

func TestHTTPServerStopCancelsStreamingRequestContexts(t *testing.T) {
	appCtx := t.Context()

	handlerEntered := make(chan struct{})
	router := echo.New()
	router.GET("/stream", func(c *echo.Context) error {
		c.Response().Header().Set("Content-Type", "application/x-json-stream")
		c.Response().WriteHeader(http.StatusOK)
		c.Response().(http.Flusher).Flush()
		close(handlerEntered)
		<-c.Request().Context().Done()
		return nil
	})

	cfg := &config.Config{
		AgentMode: true,
		Listen:    "127.0.0.1",
		Port:      "0",
	}
	lifecycle := fxtest.NewLifecycle(t)
	srv, err := NewHTTPServer(lifecycle, HTTPServerParams{
		AppCtx: appCtx,
		Config: cfg,
		Router: router,
	})
	require.NoError(t, err)
	require.NoError(t, lifecycle.Start(t.Context()))

	resp, err := http.Get("http://" + srv.Addr + "/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	<-handlerEntered

	shutdownCtx, cancelShutdown := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelShutdown()
	start := time.Now()
	require.NoError(t, lifecycle.Stop(shutdownCtx))
	require.Less(t, time.Since(start), time.Second)
}

func TestNewHTTPServerRejectsInvalidTLSCertificate(t *testing.T) {
	cfg := &config.Config{
		AgentMode:   true,
		TLSEnabled:  true,
		TLSCertFile: t.TempDir() + "/missing.crt",
		TLSKeyFile:  t.TempDir() + "/missing.key",
	}

	_, err := NewHTTPServer(fxtest.NewLifecycle(t), HTTPServerParams{
		AppCtx: t.Context(),
		Config: cfg,
		Router: echo.New(),
	})
	require.Error(t, err)
}

func TestRegisterAppCancelHookRunsAfterLaterStopHooks(t *testing.T) {
	appCtx, cancelApp := context.WithCancelCause(t.Context())
	lifecycle := fxtest.NewLifecycle(t)
	registerAppCancelHook(lifecycle, cancelApp)
	appActiveDuringDependencyStop := false
	lifecycle.Append(fx.Hook{
		OnStop: func(context.Context) error {
			appActiveDuringDependencyStop = appCtx.Err() == nil
			return nil
		},
	})

	lifecycle.RequireStart()
	lifecycle.RequireStop()
	require.True(t, appActiveDuringDependencyStop)
	require.ErrorIs(t, appCtx.Err(), context.Canceled)
}

func TestJobSchedulerStopCancelsItsPrivateContext(t *testing.T) {
	appCtx := t.Context()

	lifecycle := fxtest.NewLifecycle(t)
	runtime := francistest.New(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&kv.KVEntry{}, &environment.Environment{}, &project.GitOpsSync{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	coordinator := runs.New(kv.NewKVService(&database.DB{DB: db}), runtime.Service(), time.UTC)
	require.NoError(t, coordinator.Register(runtime))
	admission := runs.NewAdmission(runtime.Service(), t.Name())
	require.NoError(t, admission.Register(runtime))
	workflows, err := flow.New(appCtx, runtime, coordinator, nil)
	require.NoError(t, err)
	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error { return runtime.Start(ctx, appCtx, nil) },
		OnStop:  runtime.Stop,
	})
	t.Cleanup(func() { lifecycle.RequireStop() })
	jobService := job.NewJobService(&database.DB{DB: db}, nil, &config.Config{}, coordinator, nil, nil, nil)
	gitopsSync := gitops.NewGitOpsSyncService(&database.DB{DB: db}, nil, nil, nil, nil, nil)
	require.NoError(t, db.Create(&environment.Environment{ID: "0", Name: "Local", Enabled: true}).Error)
	require.NoError(t, db.Create(&project.GitOpsSync{ID: "overdue", EnvironmentID: "0", AutoSync: true, SyncInterval: 1}).Error)
	jobScheduler, err := newJobScheduler(appCtx, lifecycle, &config.Config{}, coordinator, admission, nil, nil, nil, jobService, workflows, nil, gitopsSync, runtime)
	require.NoError(t, err)
	require.NoError(t, registerDynamicJobs(dynamicJobsParams{
		AppCtx: appCtx, Config: &config.Config{}, Scheduler: jobScheduler,
		GitOpsSync: gitopsSync, Admission: admission,
	}))
	executed := make(chan schedulertypes.Run, 1)
	coordinator.SetExecutor(func(_ context.Context, run schedulertypes.Run) (schedulertypes.Outcome, error) {
		executed <- run
		return schedulertypes.Outcome{Status: schedulertypes.Succeeded}, nil
	}, nil)
	watcher := &blockingBusWatcher{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	require.NoError(t, jobScheduler.RegisterBusWatcher(watcher, false))

	startCtx, cancelStart := context.WithTimeout(appCtx, 5*time.Second)
	defer cancelStart()
	require.NoError(t, lifecycle.Start(startCtx))
	cancelStart()
	select {
	case run := <-executed:
		require.Equal(t, "gitops-sync:overdue", run.JobID)
		require.Equal(t, "startup", run.Trigger)
		require.True(t, jobScheduler.HasJob(run.JobID))
	case <-time.After(5 * time.Second):
		require.FailNow(t, "overdue GitOps startup job did not execute through Francis")
	}
	select {
	case <-watcher.started:
	case <-time.After(time.Second):
		require.FailNow(t, "watcher did not start")
	}
	lifecycle.RequireStop()
	select {
	case <-watcher.stopped:
	case <-time.After(time.Second):
		require.FailNow(t, "watcher did not stop")
	}
	require.NoError(t, appCtx.Err())
}

func TestApplicationOptionsValidate(t *testing.T) {
	appCtx, cancelApp := context.WithCancelCause(t.Context())
	defer cancelApp(nil)

	err := fx.ValidateApp(applicationOptions(
		appCtx,
		&config.Config{},
		(*database.DB)(nil),
		cancelApp,
	))
	require.NoError(t, err)
}

func TestPrepareServerTLS_AgentModeSkipsManagerMTLSValidation(t *testing.T) {
	cfg := &config.Config{
		AgentMode:     true,
		EdgeMTLSMode:  "required",
		ManagerApiUrl: "https://127.0.0.1:3552",
	}

	useTLS, tlsCertFile, tlsKeyFile, edgeCfg, err := prepareServerTLSInternal(t.Context(), cfg)
	require.NoError(t, err)
	assert.False(t, useTLS)
	assert.Empty(t, tlsCertFile)
	assert.Empty(t, tlsKeyFile)
	require.NotNil(t, edgeCfg)
	assert.Equal(t, "required", edgeCfg.EdgeMTLSMode)
}

func TestPrepareServerTLS_AllowsExternalMTLSTermination(t *testing.T) {
	libcrypto.InitEncryption(&libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-edge-mtls-32bytes-min",
		Environment:   "test",
	})

	assetsDir := t.TempDir()
	cfg := &config.Config{
		TLSEnabled:        false,
		EdgeMTLSMode:      "required",
		EdgeMTLSAssetsDir: assetsDir,
		EncryptionKey:     "test-encryption-key-for-edge-mtls-32bytes-min",
	}

	useTLS, tlsCertFile, tlsKeyFile, edgeCfg, err := prepareServerTLSInternal(t.Context(), cfg)
	require.NoError(t, err)
	assert.False(t, useTLS)
	assert.Empty(t, tlsCertFile)
	assert.Empty(t, tlsKeyFile)
	require.NotNil(t, edgeCfg)
	assert.Equal(t, "required", edgeCfg.EdgeMTLSMode)
	require.FileExists(t, edgeCfg.EdgeMTLSCAFile)
	require.FileExists(t, assetsDir+"/ca.crt")
	require.FileExists(t, assetsDir+"/ca.key")
}

func TestIsWeakProductionEncryptionKey(t *testing.T) {
	assert.True(t, isWeakProductionEncryptionKey("short", "production", false))
	assert.False(t, isWeakProductionEncryptionKey("test-encryption-key-for-edge-mtls-32bytes-min", "production", false))
	assert.False(t, isWeakProductionEncryptionKey("hex:abc", "production", false))
	assert.False(t, isWeakProductionEncryptionKey("short", "development", false))
}
