package agent

import (
	"cmp"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	tunnelpb "getarcane.dev/arcane/v3/proto/tunnel/v1"
)

type (
	registerResponseOrderConn struct {
		sendHook func(*TunnelMessage) error
		recvErr  error
	}
)

func TestTunnelServer_HandleConnect(t *testing.T) {
	tunnels := NewTunnelRegistry()
	resolver := func(ctx context.Context, token string) (string, error) {
		if token == "valid-token" {
			return "env-connected", nil
		}
		return "", errors.New("invalid token")
	}

	statusCallbackCalled := make(chan struct{}, 1)
	callback := func(ctx context.Context, envID string, connected bool) {
		if envID == "env-connected" && connected {
			select {
			case statusCallbackCalled <- struct{}{}:
			default:
			}
		}
	}

	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		Resolver:        resolver,
		StatusCallback:  callback,
		MTLS:            NewMTLS(MTLSConfig{}),
	})

	router := echo.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"
	headers := http.Header{}
	headers.Set(HeaderAgentToken, "valid-token")

	conn, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	err = wsjson.Write(t.Context(), conn, &TunnelMessage{
		Type:          MessageTypeRegister,
		AgentToken:    "valid-token",
		AgentInstance: "agent-connected",
		Capabilities:  []string{tunnelCapabilityMux, TunnelCapabilityForwardedPermissions},
	})
	require.NoError(t, err)

	var registerResp TunnelMessage
	err = wsjson.Read(t.Context(), conn, &registerResp)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRegisterResponse, registerResp.Type)
	assert.True(t, registerResp.Accepted)
	assert.Contains(t, registerResp.Capabilities, tunnelCapabilityMuxGrant)

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = tunnels.Get("env-connected").Get()
		return ok && tunnel != nil && tunnel.SessionID != ""
	}, time.Second, 10*time.Millisecond)
	assert.Equal(t, "agent-connected", tunnel.AgentInstance)

	select {
	case <-statusCallbackCalled:
	case <-time.After(1 * time.Second):
		require.FailNow(t, "timeout waiting for status callback")
	}

	// Clean up; close the client first so the server-side close handshake
	// does not wait on a peer that is no longer reading.
	_ = conn.CloseNow()
	tunnels.Unregister("env-connected")
}

func TestTunnelServer_HandleConnect_AcceptsTokenAfterProxyTerminatedMTLS(t *testing.T) {
	tunnels := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		Resolver: func(ctx context.Context, token string) (string, error) {
			if token == "valid-token" {
				return "env-proxy-mtls", nil
			}
			return "", errors.New("invalid token")
		},
		MTLS: NewMTLS(MTLSConfig{Mode: MTLSModeRequired, AppURL: "https://manager.example.com"}),
	})

	router := echo.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"
	headers := http.Header{}
	headers.Set(HeaderAgentToken, "valid-token")

	conn, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	err = wsjson.Write(t.Context(), conn, &TunnelMessage{
		Type:          MessageTypeRegister,
		AgentToken:    "valid-token",
		AgentInstance: "agent-proxy-mtls",
		Capabilities:  []string{tunnelCapabilityMux, TunnelCapabilityForwardedPermissions},
	})
	require.NoError(t, err)

	var registerResp TunnelMessage
	err = wsjson.Read(t.Context(), conn, &registerResp)
	require.NoError(t, err)
	require.Equal(t, MessageTypeRegisterResponse, registerResp.Type)
	require.True(t, registerResp.Accepted)
	require.Equal(t, "token", registerResp.SecurityMode)

	_ = conn.CloseNow()
	tunnels.Unregister("env-proxy-mtls")
}

func TestTunnelServer_HandleConnect_InvalidToken(t *testing.T) {
	tunnels := NewTunnelRegistry()
	resolver := func(ctx context.Context, token string) (string, error) {
		return "", errors.New("invalid")
	}

	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		Resolver:        resolver,
		MTLS:            NewMTLS(MTLSConfig{}),
	})
	router := echo.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"
	headers := http.Header{}
	headers.Set(HeaderAgentToken, "bad-token")

	conn, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	err = wsjson.Write(t.Context(), conn, &TunnelMessage{Type: MessageTypeRegister, AgentToken: "bad-token"})
	require.NoError(t, err)

	var registerResp TunnelMessage
	err = wsjson.Read(t.Context(), conn, &registerResp)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRegisterResponse, registerResp.Type)
	assert.False(t, registerResp.Accepted)
	assert.Equal(t, "invalid agent token", registerResp.Error)
}

func TestTunnelServer_HandleConnect_PrefersHeaderTokenOverRegisterMessage(t *testing.T) {
	tunnels := NewTunnelRegistry()
	var resolvedToken string
	resolver := func(ctx context.Context, token string) (string, error) {
		resolvedToken = token
		if token == "valid-token" {
			return "env-header-token", nil
		}
		return "", errors.New("invalid token")
	}

	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		Resolver:        resolver,
		MTLS:            NewMTLS(MTLSConfig{}),
	})
	router := echo.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"
	headers := http.Header{}
	headers.Set(HeaderAgentToken, "valid-token")

	conn, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	err = wsjson.Write(t.Context(), conn, &TunnelMessage{Type: MessageTypeRegister, AgentToken: "bad-token", Capabilities: []string{tunnelCapabilityMux}})
	require.NoError(t, err)

	var registerResp TunnelMessage
	err = wsjson.Read(t.Context(), conn, &registerResp)
	require.NoError(t, err)
	require.Equal(t, MessageTypeRegisterResponse, registerResp.Type)
	require.True(t, registerResp.Accepted)
	require.Equal(t, "valid-token", resolvedToken)

	_ = conn.CloseNow()
	tunnels.Unregister("env-header-token")
}

func TestTunnelServer_HandleConnect_NoToken(t *testing.T) {
	tunnels := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		Registry:        tunnels,
		MTLS:            NewMTLS(MTLSConfig{}),
		StreamWindowMiB: testStreamWindowMiB,
	})
	router := echo.New()
	router.GET("/connect", server.HandleConnect)

	ts := httptest.NewServer(router)
	defer ts.Close()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/connect"

	conn, resp, err := websocket.Dial(t.Context(), url, nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	err = wsjson.Write(t.Context(), conn, &TunnelMessage{Type: MessageTypeRegister})
	require.NoError(t, err)

	var registerResp TunnelMessage
	err = wsjson.Read(t.Context(), conn, &registerResp)
	require.NoError(t, err)
	require.Equal(t, MessageTypeRegisterResponse, registerResp.Type)
	require.False(t, registerResp.Accepted)
	require.Equal(t, "agent token required", registerResp.Error)
}

func TestTunnelServer_resolveEnvironment_TrimsToken(t *testing.T) {
	tunnels := NewTunnelRegistry()
	var resolvedToken string
	server := NewTunnelServer(TunnelServerOptions{
		StreamWindowMiB: testStreamWindowMiB,
		Registry:        tunnels,
		Resolver: func(ctx context.Context, token string) (string, error) {
			resolvedToken = token
			return "env-trimmed", nil
		},
		MTLS: NewMTLS(MTLSConfig{}),
	})

	envID, err := server.resolveEnvironment(t.Context(), "  valid-token  ")
	require.NoError(t, err)
	assert.Equal(t, "env-trimmed", envID)
	assert.Equal(t, "valid-token", resolvedToken)
}

func TestTunnelServer_resolveEnvironment_Errors(t *testing.T) {
	tunnels := NewTunnelRegistry()
	t.Run("missing resolver", func(t *testing.T) {
		server := NewTunnelServer(TunnelServerOptions{
			Registry:        tunnels,
			MTLS:            NewMTLS(MTLSConfig{}),
			StreamWindowMiB: testStreamWindowMiB,
		})
		_, err := server.resolveEnvironment(t.Context(), "token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent resolver is not configured")
	})

	t.Run("missing token", func(t *testing.T) {
		server := NewTunnelServer(TunnelServerOptions{
			StreamWindowMiB: testStreamWindowMiB,
			Registry:        tunnels,
			Resolver: func(ctx context.Context, token string) (string, error) {
				return "env", nil
			},
			MTLS: NewMTLS(MTLSConfig{}),
		})

		_, err := server.resolveEnvironment(t.Context(), "   ")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "agent token required")
	})
}

func TestAgentToken(t *testing.T) {
	t.Run("prefers agent token and trims whitespace", func(t *testing.T) {
		md := metadata.Pairs(
			strings.ToLower(HeaderAgentToken), "  agent-token  ",
			strings.ToLower(HeaderAPIKey), "api-token",
		)

		token, source := agentToken(md.Get)
		assert.Equal(t, "agent-token", token)
		assert.Equal(t, HeaderAgentToken, source)
	})

	t.Run("falls back to api key", func(t *testing.T) {
		md := metadata.Pairs(strings.ToLower(HeaderAPIKey), "api-token")

		token, source := agentToken(md.Get)
		assert.Equal(t, "api-token", token)
		assert.Equal(t, HeaderAPIKey, source)
	})

	t.Run("returns empty when metadata missing", func(t *testing.T) {
		var md metadata.MD
		token, source := agentToken(md.Get)
		assert.Empty(t, token)
		assert.Empty(t, source)
	})
}

func TestIsExpectedTunnelReceiveError(t *testing.T) {
	t.Run("nil is not expected", func(t *testing.T) {
		assert.False(t, isExpectedReceiveError(nil))
	})

	t.Run("io eof expected", func(t *testing.T) {
		assert.True(t, isExpectedReceiveError(io.EOF))
	})

	t.Run("context canceled expected", func(t *testing.T) {
		assert.True(t, isExpectedReceiveError(context.Canceled))
	})

	t.Run("context deadline expected", func(t *testing.T) {
		assert.True(t, isExpectedReceiveError(context.DeadlineExceeded))
	})

	t.Run("grpc canceled expected", func(t *testing.T) {
		err := status.Error(codes.Canceled, "context canceled")
		assert.True(t, isExpectedReceiveError(err))
	})

	t.Run("grpc deadline exceeded expected", func(t *testing.T) {
		err := status.Error(codes.DeadlineExceeded, "deadline exceeded")
		assert.True(t, isExpectedReceiveError(err))
	})

	t.Run("unexpected error not expected", func(t *testing.T) {
		assert.False(t, isExpectedReceiveError(errors.New("boom")))
	})
}

func (f *registerResponseOrderConn) Send(msg *TunnelMessage) error {
	if f.sendHook != nil {
		return f.sendHook(msg)
	}
	return nil
}

func (f *registerResponseOrderConn) Receive() (*TunnelMessage, error) {
	if f.recvErr != nil {
		return nil, f.recvErr
	}
	return nil, io.EOF
}

func (f *registerResponseOrderConn) muxConn(context.Context) *muxConn {
	return newMuxConn(
		func([]byte) error {
			return nil
		},
		func() ([]byte, error) {
			return nil, f.recvErr
		},
		f.Close,
	)
}

func (f *registerResponseOrderConn) Close() error { return nil }

func (f *registerResponseOrderConn) IsClosed() bool { return false }

func (f *registerResponseOrderConn) Transport() string { return TransportGRPC }

func TestTunnelServer_ManageConnectedTunnel_RegistersBeforeSendingGRPCRegisterResponse(t *testing.T) {
	tunnels := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		Registry:        tunnels,
		MTLS:            NewMTLS(MTLSConfig{}),
		StreamWindowMiB: testStreamWindowMiB,
	})
	envID := "env-grpc-register-order"
	t.Cleanup(func() { server.registry.Unregister(envID) })

	conn := &registerResponseOrderConn{recvErr: io.EOF}
	conn.sendHook = func(msg *TunnelMessage) error {
		require.Equal(t, MessageTypeRegisterResponse, msg.Type)
		_, ok := server.registry.Get(envID).Get()
		assert.True(t, ok, "gRPC tunnel should already be registered when the register response is sent")
		return nil
	}

	server.manageConnectedTunnel(t.Context(), &TunnelMessage{Type: MessageTypeRegister, Capabilities: []string{tunnelCapabilityMux, TunnelCapabilityForwardedPermissions}}, AgentTunnelConfig{
		Conn:          conn,
		EnvironmentID: envID,
		SecurityMode:  "token",
	})

	_, ok := server.registry.Get(envID).Get()
	assert.False(t, ok)
}

func TestTunnelServer_ManageConnectedTunnel_UnregistersAfterConnectionContextCancellation(t *testing.T) {
	registry := NewTunnelRegistry()
	server := NewTunnelServer(TunnelServerOptions{
		Registry:        registry,
		MTLS:            NewMTLS(MTLSConfig{}),
		StreamWindowMiB: testStreamWindowMiB,
	})
	envID := "env-cancelled-disconnect"
	conn := &registerResponseOrderConn{recvErr: context.Canceled}

	connectionCtx, cancelConnection := context.WithCancel(t.Context())
	cancelConnection()
	server.manageConnectedTunnel(connectionCtx, &TunnelMessage{Type: MessageTypeRegister, Capabilities: []string{tunnelCapabilityMux, TunnelCapabilityForwardedPermissions}}, AgentTunnelConfig{
		Conn:          conn,
		EnvironmentID: envID,
		SecurityMode:  "token",
	})

	_, ok := registry.Get(envID).Get()
	require.False(t, ok)

	stopCtx, cancelStop := context.WithTimeout(t.Context(), time.Second)
	defer cancelStop()
	require.NoError(t, registry.Stop(stopCtx))
}

func TestGRPCTunnelHandler(t *testing.T) {
	_, proxyNet, err := net.ParseCIDR("10.0.0.0/8")
	require.NoError(t, err)
	fullMethodPath := tunnelpb.TunnelService_Connect_FullMethodName

	tests := []struct {
		headers        http.Header
		name           string
		method         string
		path           string
		remoteAddr     string
		wantPath       string
		wantRemoteAddr string
		protoMajor     int
		wantGRPC       bool
		wantSame       bool
	}{
		{
			name:     "grpc content-type",
			method:   http.MethodPost,
			path:     "/any/path",
			headers:  http.Header{"Content-Type": {"application/grpc"}},
			wantGRPC: true,
			wantPath: "/any/path",
			wantSame: true,
		},
		{
			name:     "grpc-web content-type",
			method:   http.MethodPost,
			path:     "/any/path",
			headers:  http.Header{"Content-Type": {"application/grpc-web+proto"}},
			wantGRPC: true,
			wantPath: "/any/path",
			wantSame: true,
		},
		{
			name:     "method path without grpc content-type keeps the request",
			method:   http.MethodPost,
			path:     fullMethodPath,
			wantGRPC: true,
			wantPath: fullMethodPath,
			wantSame: true,
		},
		{
			name:     "api prefix is removed",
			method:   http.MethodPost,
			path:     "/api" + fullMethodPath,
			wantGRPC: true,
			wantPath: fullMethodPath,
		},
		{
			name:     "nested proxy prefix is removed up to method path",
			method:   http.MethodPost,
			path:     "/agent/proxy/api" + fullMethodPath,
			wantGRPC: true,
			wantPath: fullMethodPath,
		},
		{
			name:     "tunnel connect path is rewritten to the gRPC method",
			method:   http.MethodPost,
			path:     "/api/tunnel/connect",
			headers:  http.Header{"Content-Type": {"application/grpc"}},
			wantGRPC: true,
			wantPath: fullMethodPath,
		},
		{
			name:     "nested proxy tunnel connect path is rewritten",
			method:   http.MethodPost,
			path:     "/agent/proxy/api/tunnel/connect",
			headers:  http.Header{"Content-Type": {"application/grpc"}},
			wantGRPC: true,
			wantPath: fullMethodPath,
		},
		{
			name:   "trusted proxy forwards the client address",
			method: http.MethodPost,
			path:   fullMethodPath,
			headers: http.Header{
				"Content-Type":    {"application/grpc"},
				"X-Forwarded-For": {"203.0.113.9"},
			},
			remoteAddr:     "10.1.2.3:4567",
			wantGRPC:       true,
			wantPath:       fullMethodPath,
			wantRemoteAddr: "203.0.113.9:4567",
		},
		{
			name:   "untrusted peer keeps its own address",
			method: http.MethodPost,
			path:   fullMethodPath,
			headers: http.Header{
				"Content-Type":    {"application/grpc"},
				"X-Forwarded-For": {"203.0.113.9"},
			},
			remoteAddr:     "198.51.100.7:4567",
			wantGRPC:       true,
			wantPath:       fullMethodPath,
			wantRemoteAddr: "198.51.100.7:4567",
			wantSame:       true,
		},
		{
			name:    "regular api request",
			method:  http.MethodPost,
			path:    "/api/agents/pair",
			headers: http.Header{"Content-Type": {"application/json"}},
		},
		{
			name:    "requires post",
			method:  http.MethodGet,
			path:    fullMethodPath,
			headers: http.Header{"Content-Type": {"application/grpc"}},
		},
		{
			name:   "http2 post with te trailers and json content-type",
			method: http.MethodPost,
			path:   "/api/auth/login",
			headers: http.Header{
				"Content-Type": {"application/json"},
				"Te":           {"trailers"},
			},
			protoMajor: 2,
		},
		{
			name:   "http2 post with te trailers and form content-type",
			method: http.MethodPost,
			path:   "/logout",
			headers: http.Header{
				"Content-Type": {"application/x-www-form-urlencoded"},
				"Te":           {"trailers"},
			},
			protoMajor: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			for key, values := range tt.headers {
				req.Header[key] = values
			}
			req.RemoteAddr = cmp.Or(tt.remoteAddr, req.RemoteAddr)
			req.ProtoMajor = cmp.Or(tt.protoMajor, req.ProtoMajor)
			wantRemoteAddr := cmp.Or(tt.wantRemoteAddr, req.RemoteAddr)

			var grpcReq, nextReq *http.Request
			GRPCTunnelHandler(GRPCTunnelHandlerConfig{
				GRPC: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					grpcReq = r
				}),
				Next: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
					nextReq = r
				}),
				ExtractIP: echo.ExtractIPFromXFFHeader(
					echo.TrustLoopback(false),
					echo.TrustLinkLocal(false),
					echo.TrustPrivateNet(false),
					echo.TrustIPRange(proxyNet),
				),
			}).ServeHTTP(httptest.NewRecorder(), req)

			if !tt.wantGRPC {
				assert.Nil(t, grpcReq)
				assert.Same(t, req, nextReq)
				return
			}
			assert.Nil(t, nextReq)
			require.NotNil(t, grpcReq)
			assert.Equal(t, tt.wantPath, grpcReq.URL.Path)
			assert.Equal(t, wantRemoteAddr, grpcReq.RemoteAddr)
			if tt.wantSame {
				assert.Same(t, req, grpcReq)
				return
			}
			assert.NotSame(t, req, grpcReq)
			assert.Equal(t, tt.wantPath, grpcReq.RequestURI)
		})
	}
}
