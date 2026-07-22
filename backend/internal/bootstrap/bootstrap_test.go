package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/api"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	libcrypto "go.getarcane.app/sys/crypto"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"golang.org/x/net/http2"
)

type blockingBusWatcherInternal struct {
	started chan struct{}
	stopped chan struct{}
}

func (w *blockingBusWatcherInternal) Name() string { return "blocking" }

func (w *blockingBusWatcherInternal) Start(ctx context.Context) error {
	close(w.started)
	<-ctx.Done()
	close(w.stopped)
	return nil
}

func (w *blockingBusWatcherInternal) RunNow(context.Context) error { return nil }

func TestNormalizeTunnelGRPCRequestPathInternal(t *testing.T) {
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	t.Run("nil request", func(t *testing.T) {
		assert.Nil(t, normalizeTunnelGRPCRequestPathInternal(nil))
	})

	t.Run("path without prefix remains unchanged", func(t *testing.T) {
		req := httptest.NewRequest("POST", fullMethodPath, nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.Same(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
	})

	t.Run("api prefix is removed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api"+fullMethodPath, nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("nested proxy prefix is removed up to method path", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/edge/proxy/api"+fullMethodPath, nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("legacy /api/tunnel/connect is rewritten to gRPC method", func(t *testing.T) {
		// Regression: PR #2722 removed this branch, breaking the edge agent's
		// gRPC transport. The agent client uses /api/tunnel/connect as its
		// gRPC method path so reverse proxies can route tunnel traffic with
		// a stable URL instead of the proto-generated gRPC service name.
		req := httptest.NewRequest("POST", "/api/tunnel/connect", nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})

	t.Run("nested proxy with legacy /api/tunnel/connect is rewritten", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/edge/proxy/api/tunnel/connect", nil)
		normalized := normalizeTunnelGRPCRequestPathInternal(req)

		assert.NotSame(t, req, normalized)
		assert.Equal(t, fullMethodPath, normalized.URL.Path)
		assert.Equal(t, fullMethodPath, normalized.RequestURI)
	})
}

func TestIsTunnelGRPCRequestInternal(t *testing.T) {
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	t.Run("detects by grpc content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/any/path", nil)
		req.Header.Set("Content-Type", "application/grpc")
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by grpc-web content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/any/path", nil)
		req.Header.Set("Content-Type", "application/grpc-web+proto")
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("detects by method path without grpc content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fullMethodPath, nil)
		assert.True(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match regular api requests", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/environments/pair", nil)
		req.Header.Set("Content-Type", "application/json")
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("requires post", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fullMethodPath, nil)
		req.Header.Set("Content-Type", "application/grpc")
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match http2 post with te trailers and json content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Te", "trailers")
		req.ProtoMajor = 2
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})

	t.Run("does not match http2 post with te trailers and form content-type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/logout", nil)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Te", "trailers")
		req.ProtoMajor = 2
		assert.False(t, isTunnelGRPCRequestInternal(req))
	})
}

func TestConfigureHTTPProtocolsInternal(t *testing.T) {
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

		assert.Same(t, handler, configuredHandler)
		require.NotNil(t, protocols)
		assert.True(t, protocols.HTTP1())
		assert.False(t, protocols.HTTP2())
		assert.True(t, protocols.UnencryptedHTTP2())
	})
}

func TestHTTP2APIResponsesDoNotUseAPIGzipInternal(t *testing.T) {
	cfg := &config.Config{
		AgentMode:   true,
		AppUrl:      "http://localhost:3552",
		Environment: config.AppEnvironmentTest,
	}
	router, _ := newRouter(RouterParams{
		Context:        context.Background(),
		Config:         cfg,
		HandlerDeps:    api.HandlerDeps{},
		AuthMiddleware: middleware.NewAuthMiddleware(nil, cfg),
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
		require.NoError(t, server.Shutdown(context.Background()))
		require.ErrorIs(t, <-errCh, http.ErrServerClosed)
	})

	transport := &http2.Transport{
		AllowHTTP:          true,
		DisableCompression: true,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: transport}

	for _, path := range []string{"/api/health", "/api/openapi.json"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String()+path, nil)
			require.NoError(t, err)
			req.Header.Set("Accept-Encoding", "gzip")

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

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

func TestHTTPServerStopCancelsStreamingRequestContextsInternal(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

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
	require.NoError(t, lifecycle.Start(context.Background()))

	resp, err := http.Get("http://" + srv.Addr + "/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	<-handlerEntered

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	start := time.Now()
	require.NoError(t, lifecycle.Stop(shutdownCtx))
	require.Less(t, time.Since(start), time.Second)
}

func TestNewHTTPServerRejectsInvalidTLSCertificateInternal(t *testing.T) {
	cfg := &config.Config{
		AgentMode:   true,
		TLSEnabled:  true,
		TLSCertFile: t.TempDir() + "/missing.crt",
		TLSKeyFile:  t.TempDir() + "/missing.key",
	}

	_, err := NewHTTPServer(fxtest.NewLifecycle(t), HTTPServerParams{
		AppCtx: context.Background(),
		Config: cfg,
		Router: echo.New(),
	})
	require.Error(t, err)
}

func TestRegisterAppCancelHookRunsBeforeEarlierStopHooks(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	lifecycle := fxtest.NewLifecycle(t)
	appCanceledBeforeDependencyStop := false
	lifecycle.Append(fx.Hook{
		OnStop: func(context.Context) error {
			appCanceledBeforeDependencyStop = errors.Is(appCtx.Err(), context.Canceled)
			return nil
		},
	})
	registerAppCancelHook(lifecycle, cancelApp)

	lifecycle.RequireStart()
	lifecycle.RequireStop()
	require.True(t, appCanceledBeforeDependencyStop)
}

func TestRollbackCancelHookRunsBeforeEarlierStopsInternal(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	appCanceledBeforeDependencyStop := false
	app := fxtest.New(t,
		fx.Supply(cancelApp),
		fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStop: func(context.Context) error {
					appCanceledBeforeDependencyStop = errors.Is(appCtx.Err(), context.Canceled)
					return nil
				},
			})
		}),
		fx.Invoke(registerAppRollbackCancelHook),
		fx.Invoke(func(lc fx.Lifecycle) {
			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					return errors.New("listen failed")
				},
			})
		}),
	)

	require.Error(t, app.Start(context.Background()))
	require.True(t, appCanceledBeforeDependencyStop)
}

func TestJobSchedulerStopCancelsItsPrivateContextInternal(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	lifecycle := fxtest.NewLifecycle(t)
	jobScheduler := newJobScheduler(appCtx, lifecycle, &config.Config{}, nil, nil, nil)
	watcher := &blockingBusWatcherInternal{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	jobScheduler.RegisterBusWatcher(watcher, false)

	lifecycle.RequireStart()
	select {
	case <-watcher.started:
	case <-time.After(time.Second):
		t.Fatal("watcher did not start")
	}
	lifecycle.RequireStop()
	select {
	case <-watcher.stopped:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop")
	}
	require.NoError(t, appCtx.Err())
}

func TestApplicationOptionsValidate(t *testing.T) {
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()

	err := fx.ValidateApp(applicationOptions(
		appCtx,
		&config.Config{},
		(*database.DB)(nil),
		cancelApp,
	))
	require.NoError(t, err)
}

func TestPrepareServerTLSInternal_AgentModeSkipsManagerMTLSValidation(t *testing.T) {
	cfg := &config.Config{
		AgentMode:     true,
		EdgeMTLSMode:  "required",
		ManagerApiUrl: "https://127.0.0.1:3552",
	}

	useTLS, tlsCertFile, tlsKeyFile, edgeCfg, err := prepareServerTLSInternal(context.Background(), cfg)
	require.NoError(t, err)
	assert.False(t, useTLS)
	assert.Empty(t, tlsCertFile)
	assert.Empty(t, tlsKeyFile)
	require.NotNil(t, edgeCfg)
	assert.Equal(t, "required", edgeCfg.EdgeMTLSMode)
}

func TestPrepareServerTLSInternal_AllowsExternalMTLSTermination(t *testing.T) {
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

	useTLS, tlsCertFile, tlsKeyFile, edgeCfg, err := prepareServerTLSInternal(context.Background(), cfg)
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

func TestIsWeakProductionEncryptionKeyInternal(t *testing.T) {
	assert.True(t, isWeakProductionEncryptionKeyInternal("short", "production", false))
	assert.False(t, isWeakProductionEncryptionKeyInternal("test-encryption-key-for-edge-mtls-32bytes-min", "production", false))
	assert.False(t, isWeakProductionEncryptionKeyInternal("hex:abc", "production", false))
	assert.False(t, isWeakProductionEncryptionKeyInternal("short", "development", false))
}
