package edge

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

type stallingTunnelService struct {
	tunnelpb.UnimplementedTunnelServiceServer
	connectCount atomic.Int32
}

type blockingRegistrationConn struct {
	closeCount atomic.Int32
	closedCh   chan struct{}
}

func newBlockingRegistrationConn() *blockingRegistrationConn {
	return &blockingRegistrationConn{closedCh: make(chan struct{})}
}

func (c *blockingRegistrationConn) Send(*TunnelMessage) error { return nil }

func (c *blockingRegistrationConn) Receive() (*TunnelMessage, error) {
	<-c.closedCh
	return nil, io.EOF
}

func (c *blockingRegistrationConn) IsExpectedReceiveError(error) bool { return false }

func (c *blockingRegistrationConn) Close() error {
	if c.closeCount.Add(1) == 1 {
		close(c.closedCh)
	}
	return nil
}

func (c *blockingRegistrationConn) IsClosed() bool {
	select {
	case <-c.closedCh:
		return true
	default:
		return false
	}
}

func (c *blockingRegistrationConn) SendRequest(context.Context, *TunnelMessage, *sync.Map) (*TunnelMessage, error) {
	return nil, errors.New("not implemented")
}

func (s *stallingTunnelService) Connect(stream grpc.BidiStreamingServer[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]) error {
	s.connectCount.Add(1)
	if _, err := stream.Recv(); err != nil {
		return err
	}

	<-stream.Context().Done()
	return stream.Context().Err()
}

func TestTunnelClient_GRPC_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	envID := "env-e2e-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/local/health" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-from-agent"))
	})

	cfg := &Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         managerURL,
		AgentToken:            "valid-token",
		EdgeReconnectInterval: 1,
		Port:                  "3552",
	}

	client := NewTunnelClient(cfg, localHandler)
	errCh := make(chan error, 4)
	go client.StartWithErrorChan(ctx, errCh)

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		var ok bool
		tunnel, ok = GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, 5*time.Second, 20*time.Millisecond)

	proxyCtx, proxyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer proxyCancel()

	status, headers, body, err := ProxyRequest(proxyCtx, tunnel, http.MethodGet, "/local/health", "", map[string]string{"Accept": "text/plain"}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "text/plain", headers["Content-Type"])
	assert.Equal(t, "ok-from-agent", string(body))

	select {
	case clientErr := <-errCh:
		require.NoError(t, clientErr)
	default:
	}
}

func TestTunnelClient_useTLSForManagerGRPC(t *testing.T) {
	tests := []struct {
		name       string
		managerURL string
		expected   bool
	}{
		{name: "https manager url", managerURL: "https://manager.example.com/api", expected: true},
		{name: "https manager url with reverse proxy path", managerURL: "https://manager.example.com/arcane/api", expected: true},
		{name: "http manager url", managerURL: "http://manager.example.com/api", expected: false},
		{name: "invalid manager url", managerURL: "://bad-url", expected: false},
		{name: "empty manager url", managerURL: "", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewTunnelClient(&Config{ManagerApiUrl: tc.managerURL}, http.NotFoundHandler())
			assert.Equal(t, tc.expected, client.useTLSForManagerGRPC())
		})
	}
}

func TestStartTunnelClientWithErrors_GRPCValidation(t *testing.T) {
	ctx := t.Context()

	t.Run("edge mode required", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "edge tunnel disabled")
	})

	t.Run("manager url required for grpc transport", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			AgentToken:    "token",
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "MANAGER_API_URL")
	})

	t.Run("agent token required", func(t *testing.T) {
		_, err := StartTunnelClientWithErrors(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			ManagerApiUrl: "https://manager.example.com/arcane/api",
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "AGENT_TOKEN is required")
	})
}

func TestTunnelClient_connectAndServeGRPC_EmptyManagerAddress(t *testing.T) {
	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	err := client.connectAndServeGRPC(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager gRPC address is empty")
}

func TestTunnelClient_connectAndServeGRPC_RegistrationRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	envID := "env-e2e-grpc-reject-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         managerURL,
		AgentToken:            "invalid-token",
		EdgeReconnectInterval: 1,
		Port:                  "3552",
	}, http.NotFoundHandler())

	err := client.connectAndServeGRPC(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager rejected tunnel registration")
	assert.Contains(t, err.Error(), "invalid agent token")
}

func TestTunnelClient_connectAndServeGRPC_TimesOutWithoutRegisterResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	service := &stallingTunnelService{}
	managerURL, stopManager := startTestTunnelServiceOnAPIPathInternal(t, ctx, service)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.grpcRegistrationTimeout = 100 * time.Millisecond

	err := client.connectAndServeGRPC(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out waiting for gRPC tunnel registration response")
	assert.EqualValues(t, 1, service.connectCount.Load())
	assert.True(t, client.conn == nil || client.conn.IsClosed())
}

func TestTunnelClient_awaitGRPCRegistrationInternal_ClosesConnOnContextDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	conn := newBlockingRegistrationConn()
	client := &TunnelClient{
		conn:                    conn,
		grpcRegistrationTimeout: time.Second,
	}

	msg, err := client.awaitGRPCRegistrationInternal(ctx)
	require.Nil(t, msg)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, conn.IsClosed())
	assert.EqualValues(t, 1, conn.closeCount.Load())
}

func TestTunnelClient_GRPC_WebSocketProxyEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	envID := "env-e2e-grpc-ws-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	headerTokenCh := make(chan string, 1)
	queryCh := make(chan string, 1)
	receivedMsgCh := make(chan string, 1)

	localWSServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/local/ws" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		headerTokenCh <- r.Header.Get(HeaderAPIKey)
		queryCh <- r.URL.RawQuery

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			mt, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			receivedMsgCh <- string(data)
			if err := conn.WriteMessage(mt, append([]byte("local echo: "), data...)); err != nil {
				return
			}
		}
	}))
	defer localWSServer.Close()

	parsedLocalURL, err := url.Parse(localWSServer.URL)
	require.NoError(t, err)
	_, localPort, err := net.SplitHostPort(parsedLocalURL.Host)
	require.NoError(t, err)

	cfg := &Config{
		EdgeTransport:         EdgeTransportGRPC,
		ManagerApiUrl:         managerURL,
		AgentToken:            "valid-token",
		EdgeReconnectInterval: 1,
		Port:                  localPort,
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	errCh := make(chan error, 4)
	go client.StartWithErrorChan(ctx, errCh)

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, 5*time.Second, 20*time.Millisecond)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/proxy-ws", func(c *gin.Context) {
		tunnel, ok := GetRegistry().Get(envID)
		if !ok || tunnel == nil {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		ProxyWebSocketRequest(c, tunnel, "/local/ws")
	})

	proxyServer := httptest.NewServer(router)
	defer proxyServer.Close()

	proxyURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/proxy-ws?tail=100"
	proxyConn, resp, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	require.NoError(t, err)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = proxyConn.Close() }()

	require.NoError(t, proxyConn.WriteMessage(websocket.TextMessage, []byte("hello-grpc-ws")))

	msgType, payload, err := proxyConn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, msgType)
	assert.Equal(t, "local echo: hello-grpc-ws", string(payload))

	select {
	case got := <-headerTokenCh:
		assert.Equal(t, "valid-token", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for local websocket auth header")
	}

	select {
	case got := <-queryCh:
		assert.Equal(t, "tail=100", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for forwarded query")
	}

	select {
	case got := <-receivedMsgCh:
		assert.Equal(t, "hello-grpc-ws", got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for forwarded websocket payload")
	}

	select {
	case clientErr := <-errCh:
		require.NoError(t, clientErr)
	default:
	}
}

func TestTunnelClient_connectAndServe_WebSocketConfigFallsBackToWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		select {
		case wsConnectedCh <- struct{}{}:
		default:
		}

		time.Sleep(100 * time.Millisecond)
	}))
	defer managerServer.Close()

	cfg := &Config{
		EdgeTransport: EdgeTransportWebSocket,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	err := client.connectAndServe(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected websocket fallback connection to manager")
	}
}

func TestTunnelClient_connectAndServe_AutoTransportFallsBackToWebSocketWhenGRPCUnavailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		select {
		case wsConnectedCh <- struct{}{}:
		default:
		}

		time.Sleep(100 * time.Millisecond)
	}))
	defer managerServer.Close()

	cfg := &Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	err := client.connectAndServe(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected auto transport to fall back to websocket")
	}
}

func TestTunnelClient_connectAndServe_AutoTransportDerivesWebSocketFallbackURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		select {
		case wsConnectedCh <- struct{}{}:
		default:
		}

		time.Sleep(100 * time.Millisecond)
	}))
	defer managerServer.Close()

	cfg := &Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}

	client := NewTunnelClient(cfg, http.NotFoundHandler())
	client.managerURL = ""

	err := client.connectAndServe(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected auto transport to derive websocket fallback URL")
	}
}

func TestTunnelClient_connectAndServe_AutoTransportOpensGRPCWhenAvailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	envID := "env-auto-poll-grpc"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPathInternal(t, ctx, tunnelServer)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServe(ctx)
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
			return false
		}
		_, isGRPC := tunnel.Conn.(*GRPCManagerTunnelConn)
		return isGRPC
	}, 3*time.Second, 20*time.Millisecond)

	err := <-errCh
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func startTestGRPCTunnelServerOnAPIPathInternal(t *testing.T, ctx context.Context, tunnelServer *TunnelServer) (string, func()) {
	t.Helper()
	return startTestTunnelServiceOnAPIPathInternal(t, ctx, tunnelServer)
}

func startTestTunnelServiceOnAPIPathInternal(t *testing.T, ctx context.Context, service tunnelpb.TunnelServiceServer) (string, func()) {
	t.Helper()

	grpcServer := grpc.NewServer()
	tunnelpb.RegisterTunnelServiceServer(grpcServer, service)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clone := r.Clone(r.Context())
		cloneURL := *clone.URL
		if r.URL.Path == "/api/tunnel/connect" {
			cloneURL.Path = tunnelpb.TunnelService_Connect_FullMethodName
		} else {
			cloneURL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		}
		clone.URL = &cloneURL
		clone.RequestURI = cloneURL.Path
		grpcServer.ServeHTTP(w, clone)
	}), &http2.Server{})

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = httpServer.Serve(lis)
	}()

	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		grpcServer.Stop()
		_ = lis.Close()
	}

	return "http://" + lis.Addr().String(), cleanup
}
