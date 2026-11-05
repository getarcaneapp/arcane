package bootstrap

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/libtnb/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"

	libcrypto "kit.getarcane.app/sys/crypto"

	"getarcane.dev/arcane/v3/api"
	"getarcane.dev/arcane/v3/internal/auth"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/health"
	"getarcane.dev/arcane/v3/internal/project"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/swarm"
	"getarcane.dev/arcane/v3/internal/system"
	"getarcane.dev/arcane/v3/pkg/agent"
)

func newTestSettingsModule(t *testing.T, cfg *config.Config) *settings.Module {
	t.Helper()
	return settings.New(settings.ModuleParams{
		Service: newTestSettingsService(t, cfg, settings.Env{}),
		Config:  cfg,
		MTLS:    agent.NewMTLS(agent.MTLSConfig{}),
	})
}

func newTestSettingsService(t *testing.T, cfg *config.Config, env settings.Env) *settings.SettingsService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     &database.DB{DB: db},
		Config: cfg,
		Env:    env,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context())))
	})
	return settingsService
}

func startTestHTTPServer(t *testing.T, cfg *config.Config, router *echo.Echo) string {
	t.Helper()
	lc := fxtest.NewLifecycle(t)
	srv, err := NewHTTPServer(lc, HTTPServerParams{
		AppCtx:          t.Context(),
		Config:          cfg,
		Router:          router,
		TunnelServer:    agent.NewTunnelServer(agent.TunnelServerOptions{Registry: agent.NewTunnelRegistry(), MTLS: agent.NewMTLS(agent.MTLSConfig{})}),
		SettingsService: newTestSettingsService(t, cfg, settings.Env{}),
	})
	require.NoError(t, err)
	lc.RequireStart()
	t.Cleanup(func() { lc.RequireStop() })
	return srv.Addr
}

func TestHTTPServerProxyProtocol(t *testing.T) {
	router := echo.New()
	router.GET("/addr", func(c *echo.Context) error {
		host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, host)
	})
	addr := startTestHTTPServer(t, &config.Config{
		Listen:         "127.0.0.1",
		Port:           "0",
		TrustedProxies: "127.0.0.1/32",
	}, router)

	for _, tt := range []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "trusted proxy header sets the client address", prefix: "PROXY TCP4 203.0.113.9 127.0.0.1 4567 3552\r\n", want: "203.0.113.9"},
		{name: "trusted proxy without header keeps the peer address", want: "127.0.0.1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := net.Dial("tcp", addr)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			_, err = conn.Write([]byte(tt.prefix + "GET /addr HTTP/1.1\r\nHost: arcane\r\nConnection: close\r\n\r\n"))
			require.NoError(t, err)
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(body))
		})
	}
}

func TestHTTP2APIResponsesDoNotUseAPIGzip(t *testing.T) {
	cfg := &config.Config{
		AgentEnabled: true,
		AppUrl:       "http://localhost:3552",
		Environment:  config.AppEnvironmentTest,
	}
	router := newRouter(RouterParams{
		Context: t.Context(),
		Config:  cfg,
		HandlerDeps: api.HandlerDeps{
			Project:   project.New(&project.ProjectService{}, nil),
			Container: container.New(&container.ContainerService{}, nil, nil, nil),
			Swarm:     swarm.New(&swarm.SwarmService{}, nil, nil, nil),
			System:    system.New(&system.SystemService{}, nil, nil),
			Health:    health.New(&health.Service{}),
			Settings:  newTestSettingsModule(t, cfg),
		},
		AuthMiddleware: auth.NewAuthMiddleware(nil, cfg),
		TunnelRegistry: agent.NewTunnelRegistry(),
	})
	addr := startTestHTTPServer(t, &config.Config{
		Listen: "127.0.0.1",
		Port:   "0",
	}, router)

	clientProtocols := &http.Protocols{}
	clientProtocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Protocols: clientProtocols, DisableCompression: true}
	client := &http.Client{Transport: transport}

	for _, path := range []string{"/api/health", "/api/openapi.json"} {
		t.Run(path, func(t *testing.T) {
			req, requestErr := http.NewRequest(http.MethodGet, "http://"+addr+path, http.NoBody)
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
	release := make(chan struct{})
	router := echo.New()
	router.GET("/stream", func(c *echo.Context) error {
		c.Response().WriteHeader(http.StatusOK)
		if err := http.NewResponseController(c.Response()).Flush(); err != nil {
			return err
		}
		<-release
		_, err := c.Response().Write([]byte("done"))
		return err
	})
	addr := startTestHTTPServer(t, &config.Config{
		Listen: "127.0.0.1",
		Port:   "0",
	}, router)

	clientProtocols := &http.Protocols{}
	clientProtocols.SetUnencryptedHTTP2(true)
	client := &http.Client{Transport: &http.Transport{Protocols: clientProtocols}}

	resp, err := client.Get("http://" + addr + "/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, "HTTP/2.0", resp.Proto)

	time.Sleep(6 * time.Second)
	close(release)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "done", string(body))
}

func TestNewHTTPServerRejectsInvalidTLSCertificate(t *testing.T) {
	cfg := &config.Config{
		TLSEnabled:  true,
		TLSCertFile: t.TempDir() + "/missing.crt",
		TLSKeyFile:  t.TempDir() + "/missing.key",
	}

	_, err := NewHTTPServer(fxtest.NewLifecycle(t), HTTPServerParams{
		AppCtx:          t.Context(),
		Config:          cfg,
		Router:          echo.New(),
		TunnelServer:    agent.NewTunnelServer(agent.TunnelServerOptions{Registry: agent.NewTunnelRegistry(), MTLS: agent.NewMTLS(agent.MTLSConfig{})}),
		SettingsService: newTestSettingsService(t, cfg, settings.Env{}),
	})
	require.Error(t, err)
}

func TestNewHTTPServerHTTP2ReceiveWindowFromSettings(t *testing.T) {
	tests := []struct {
		env            settings.Env
		name           string
		wantStream     int
		wantConnection int
	}{
		{
			name:           "default",
			env:            settings.Env{},
			wantStream:     32 << 20,
			wantConnection: 64 << 20,
		},
		{
			name:           "minimum",
			env:            settings.Env{"http2ReceiveWindowMb": "1"},
			wantStream:     1 << 20,
			wantConnection: 2 << 20,
		},
		{
			name:           "maximum",
			env:            settings.Env{"http2ReceiveWindowMb": "256"},
			wantStream:     256 << 20,
			wantConnection: 512 << 20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Listen: "127.0.0.1",
				Port:   "0",
			}
			srv, err := NewHTTPServer(fxtest.NewLifecycle(t), HTTPServerParams{
				AppCtx:          t.Context(),
				Config:          cfg,
				Router:          echo.New(),
				TunnelServer:    agent.NewTunnelServer(agent.TunnelServerOptions{Registry: agent.NewTunnelRegistry(), MTLS: agent.NewMTLS(agent.MTLSConfig{})}),
				SettingsService: newTestSettingsService(t, cfg, tt.env),
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantStream, srv.HTTP2.MaxReceiveBufferPerStream)
			assert.Equal(t, tt.wantConnection, srv.HTTP2.MaxReceiveBufferPerConnection)
		})
	}
}

func TestApplicationOptionsValidate(t *testing.T) {
	appCtx, cancelApp := context.WithCancelCause(t.Context())
	defer cancelApp(nil)

	err := fx.ValidateApp(applicationOptions(
		appCtx,
		&config.Config{},
		settings.Env{},
		(*database.DB)(nil),
		"test-encryption-key",
		cancelApp,
		&telemetryProviders{},
	))
	require.NoError(t, err)
}

func TestPrepareServerTLS_AllowsExternalMTLSTermination(t *testing.T) {
	libcrypto.InitEncryption(libcrypto.Config{
		EncryptionKey: "test-encryption-key-for-agent-mtls-32bytes-min",
		Environment:   "test",
	})

	assetsDir := t.TempDir()
	cfg := &config.Config{
		TLSEnabled:         false,
		AgentMTLSMode:      "required",
		AgentMTLSAssetsDir: assetsDir,
		EncryptionKey:      "test-encryption-key-for-agent-mtls-32bytes-min",
	}

	mtls := buildAgentRuntimeConfig(cfg, "", agent.AgentHostInfo{}).MTLS
	useTLS, tlsCertFile, tlsKeyFile, agentTLSConfig, err := prepareServerTLS(t.Context(), cfg, mtls)
	require.NoError(t, err)
	assert.False(t, useTLS)
	assert.Empty(t, tlsCertFile)
	assert.Empty(t, tlsKeyFile)
	require.NotNil(t, agentTLSConfig)
	assert.Equal(t, agent.MTLSModeRequired, mtls.Mode)
	require.FileExists(t, mtls.CAFile)
	require.FileExists(t, assetsDir+"/ca.crt")
	require.FileExists(t, assetsDir+"/ca.key")
}
