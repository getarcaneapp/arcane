package bootstrap

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/di"
	"github.com/getarcaneapp/arcane/backend/v2/internal/middleware"
	libcrypto "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/crypto"
	tunnelpb "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

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
		AppUrl:      "http://localhost:3552",
		Environment: config.AppEnvironmentTest,
	}
	router, _ := setupRouter(context.Background(), cfg, &di.Services{
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

func TestShutdownCancelsStreamingRequestContextsInternal(t *testing.T) {
	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	handlerEntered := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-json-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		close(handlerEntered)
		// Streaming handlers block until their request context ends; Shutdown
		// alone never cancels it, so this models an open activity stream.
		<-r.Context().Done()
	})

	srv, err := newHTTPServerInternal(baseCtx, "127.0.0.1:0", handler, nil, false, nil)
	require.NoError(t, err)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()

	resp, err := http.Get("http://" + listener.Addr().String() + "/stream")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	<-handlerEntered

	cancelBase()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, srv.Shutdown(shutdownCtx))
	require.Less(t, time.Since(start), time.Second)
	require.ErrorIs(t, <-errCh, http.ErrServerClosed)
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
