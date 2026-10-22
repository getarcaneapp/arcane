package edge

import (
	"context"
	"encoding/json"
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

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tunnelpb "github.com/getarcaneapp/arcane/backend/v3/proto/tunnel/v1"
)

type stallingTunnelService struct {
	tunnelpb.UnimplementedTunnelServiceServer
	connectCount atomic.Int32
}

type blockingRegistrationConn struct {
	closeCount       atomic.Int32
	closedCh         chan struct{}
	receiveStarted   chan struct{}
	receiveStartOnce sync.Once
}

func newBlockingRegistrationConn() *blockingRegistrationConn {
	return &blockingRegistrationConn{
		closedCh:       make(chan struct{}),
		receiveStarted: make(chan struct{}),
	}
}

func (c *blockingRegistrationConn) Send(*TunnelMessage) error { return nil }

func (c *blockingRegistrationConn) Receive() (*TunnelMessage, error) {
	c.receiveStartOnce.Do(func() {
		close(c.receiveStarted)
	})
	<-c.closedCh
	return nil, io.EOF
}

func (c *blockingRegistrationConn) muxConn(context.Context) io.ReadWriteCloser { return nil }

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

func (c *blockingRegistrationConn) Transport() string { return EdgeTransportWebSocket }

func (s *stallingTunnelService) Connect(stream grpc.BidiStreamingServer[tunnelpb.AgentMessage, tunnelpb.ManagerMessage]) error {
	s.connectCount.Add(1)
	if _, err := stream.Recv(); err != nil {
		return err
	}

	<-stream.Context().Done()
	return stream.Context().Err()
}

func TestTunnelClient_GRPC_EndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	envID := "env-e2e-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServerWithRegistry(GetRegistry(), resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPath(t, ctx, tunnelServer)
	defer stopManager()

	localHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
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
		tunnel, ok = GetRegistry().Get(envID).Get()
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, 5*time.Second, 20*time.Millisecond)

	proxyCtx, proxyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer proxyCancel()

	localStatus, headers, body, err := ProxyRequest(proxyCtx, tunnel, http.MethodGet, "/api/health", "", map[string]string{"Accept": "text/plain"}, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, localStatus)
	assert.Equal(t, "text/plain", headers["Content-Type"])
	assert.Equal(t, "ok-from-agent", string(body))

	select {
	case clientErr := <-errCh:
		require.NoError(t, clientErr)
	default:
	}
}

func TestManagerUsesTLS(t *testing.T) {
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
			assert.Equal(t, tc.expected, managerUsesTLS(&Config{ManagerApiUrl: tc.managerURL}))
		})
	}
}

func TestStartTunnelClient_GRPCValidation(t *testing.T) {
	ctx := t.Context()

	t.Run("edge mode required", func(t *testing.T) {
		_, err := StartTunnelClient(ctx, &Config{}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "edge tunnel disabled")
	})

	t.Run("manager url required for grpc transport", func(t *testing.T) {
		_, err := StartTunnelClient(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			AgentToken:    "token",
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ARCANE_MANAGER_API_URL")
	})

	t.Run("agent token required", func(t *testing.T) {
		_, err := StartTunnelClient(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			ManagerApiUrl: "https://manager.example.com/arcane/api",
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ARCANE_AGENT_TOKEN is required")
	})

	t.Run("mtls requires https manager url", func(t *testing.T) {
		_, err := StartTunnelClient(ctx, &Config{
			EdgeAgent:     true,
			EdgeTransport: EdgeTransportGRPC,
			ManagerApiUrl: "http://manager.example.com/api",
			AgentToken:    "token",
			EdgeMTLSMode:  EdgeMTLSModeRequired,
		}, http.NotFoundHandler())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "ARCANE_MANAGER_API_URL to use https")
	})
}

func TestTunnelClient_StartWithErrorChan_RetriesMTLSEnrollmentEachAttempt(t *testing.T) {
	client := NewTunnelClient(&Config{
		EdgeAgent:         true,
		EdgeTransport:     EdgeTransportGRPC,
		ManagerApiUrl:     "https://127.0.0.1:1/api",
		AgentToken:        "token",
		EdgeMTLSMode:      EdgeMTLSModeRequired,
		EdgeMTLSAssetsDir: t.TempDir(),
	}, http.NotFoundHandler())
	client.reconnectInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go client.StartWithErrorChan(ctx, errCh)

	for range 2 {
		select {
		case err := <-errCh:
			require.ErrorContains(t, err, "edge mTLS enrollment request failed")
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for an mTLS enrollment attempt")
		}
	}
	cancel()
	require.Eventually(t, func() bool {
		_, open := <-errCh
		return !open
	}, 5*time.Second, 10*time.Millisecond)
}

func TestTunnelClient_connectAndServeGRPC_EmptyManagerAddress(t *testing.T) {
	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	err := client.connectAndServeGRPC(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manager gRPC address is empty")
}

func TestTunnelClient_connectAndServeGRPC_RegistrationRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)

	envID := "env-e2e-grpc-reject-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServerWithRegistry(GetRegistry(), resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPath(t, ctx, tunnelServer)
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
	assert.Contains(t, err.Error(), "failed to receive tunnel registration response")
	assert.Contains(t, err.Error(), "invalid agent token")
}

func TestTunnelClient_connectAndServeGRPC_TimesOutWithoutRegisterResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	service := &stallingTunnelService{}
	managerURL, stopManager := startTestTunnelServiceOnAPIPath(t, ctx, service)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.registrationTimeout = 100 * time.Millisecond

	err := client.connectAndServeGRPC(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, errTunnelRegistrationTimeout)
	assert.Contains(t, err.Error(), "timed out waiting for tunnel registration response")
	assert.Contains(t, err.Error(), "not forwarding gRPC")
	assert.EqualValues(t, 1, service.connectCount.Load())
	active := client.conn.Load()
	assert.True(t, active == nil || active.conn.IsClosed())
}

func TestTunnelClient_awaitRegistration_ClosesConnOnContextDone(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	conn := newBlockingRegistrationConn()
	client := &TunnelClient{
		registrationTimeout: time.Second,
	}
	client.conn.Store(&connBox{conn: conn})

	msg, err := client.awaitRegistration(ctx, conn)
	require.Nil(t, msg)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, conn.IsClosed())
	assert.EqualValues(t, 1, conn.closeCount.Load())
}

func TestTunnelClient_awaitRegistration_ClosesAttemptConnWhenClientConnChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	firstConn := newBlockingRegistrationConn()
	secondConn := newBlockingRegistrationConn()
	t.Cleanup(func() {
		_ = firstConn.Close()
		_ = secondConn.Close()
	})

	client := &TunnelClient{
		registrationTimeout: time.Second,
	}
	client.conn.Store(&connBox{conn: firstConn})

	type registrationResult struct {
		msg *TunnelMessage
		err error
	}
	resultCh := make(chan registrationResult, 1)
	go func() {
		msg, err := client.awaitRegistration(ctx, firstConn)
		resultCh <- registrationResult{msg: msg, err: err}
	}()

	select {
	case <-firstConn.receiveStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "registration receive did not start")
	}

	client.conn.Store(&connBox{conn: secondConn})
	cancel()

	select {
	case result := <-resultCh:
		require.Nil(t, result.msg)
		require.ErrorIs(t, result.err, context.Canceled)
	case <-time.After(time.Second):
		require.FailNow(t, "registration did not exit after cancellation")
	}

	assert.True(t, firstConn.IsClosed())
	assert.EqualValues(t, 1, firstConn.closeCount.Load())
	assert.False(t, secondConn.IsClosed())
	assert.EqualValues(t, 0, secondConn.closeCount.Load())
}

func TestTunnelClient_connectAndServeManagedTunnel_WebSocketConfigFallsBackToWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

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
	err := client.connectAndServeManagedTunnel(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "expected websocket fallback connection to manager")
	}
}

func TestTunnelClient_managedTunnelTransports_AutoEnablesGRPCAndWebSocket(t *testing.T) {
	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: "http://manager.example.com",
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	transports := client.managedTunnelTransports()
	assert.True(t, transports.grpc)
	assert.True(t, transports.websocket)
}

func TestTunnelClient_connectAndServeManagedTunnel_AutoFallsBackToWebSocketWhenGRPCUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	managerURL, wsConnectedCh, stopManager := startTestWebSocketTunnelManager(t, ctx)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	grpcAddr, releaseGRPCAddr := reserveTCPAddress(t)
	defer releaseGRPCAddr()
	client.managerGRPCAddr = grpcAddr
	client.registrationTimeout = 100 * time.Millisecond

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServeManagedTunnel(ctx)
	}()

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "expected auto transport to fall back to websocket when gRPC is unavailable")
	}

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timeout waiting for auto fallback tunnel shutdown")
	}
}

func TestTunnelClient_connectAndServeManagedTunnel_AutoFallsBackToWebSocketWhenGRPCSetupHangs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	grpcAddr, stopGRPC := startHangingTCPServer(t, ctx)
	defer stopGRPC()

	managerURL, wsConnectedCh, stopManager := startTestWebSocketTunnelManager(t, ctx)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.managerGRPCAddr = grpcAddr
	client.registrationTimeout = 100 * time.Millisecond

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServeManagedTunnel(ctx)
	}()

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "expected auto transport to fall back to websocket when gRPC setup hangs")
	}

	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timeout waiting for auto fallback tunnel shutdown")
	}
}

func TestTunnelClient_connectAndServeManagedTunnel_GRPCDoesNotFallbackToWebSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	managerURL, wsConnectedCh, stopManager := startTestWebSocketTunnelManager(t, ctx)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	grpcAddr, releaseGRPCAddr := reserveTCPAddress(t)
	defer releaseGRPCAddr()
	client.managerGRPCAddr = grpcAddr
	client.registrationTimeout = 100 * time.Millisecond

	err := client.connectAndServeManagedTunnel(ctx)
	require.Error(t, err)

	select {
	case <-wsConnectedCh:
		require.FailNow(t, "explicit gRPC transport should not fall back to websocket")
	default:
	}
}

func TestTunnelClient_connectAndServeManagedTunnel_OpensGRPCWhenAvailable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
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

	tunnelServer := NewTunnelServerWithRegistry(GetRegistry(), resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	managerURL, stopManager := startTestGRPCTunnelServerOnAPIPath(t, ctx, tunnelServer)
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportGRPC,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServeManagedTunnel(ctx)
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID).Get()
		if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
			return false
		}
		_, isGRPC := tunnel.Conn.(*GRPCManagerTunnelConn)
		return isGRPC
	}, 3*time.Second, 20*time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timeout waiting for gRPC tunnel shutdown")
	}
}

func startTestWebSocketTunnelManager(t *testing.T, ctx context.Context) (string, <-chan struct{}, func()) {
	t.Helper()

	wsConnectedCh := make(chan struct{}, 1)
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tunnel/connect" {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		if _, _, readErr := conn.Read(r.Context()); readErr != nil {
			return
		}

		registerResp, err := json.Marshal(&TunnelMessage{
			Type:          MessageTypeRegisterResponse,
			Accepted:      true,
			EnvironmentID: "env-ws-fallback",
			SessionID:     "session-ws-fallback",
			Capabilities:  []string{tunnelCapabilityMuxGrant},
		})
		if err != nil {
			return
		}
		if writeErr := conn.Write(r.Context(), websocket.MessageText, registerResp); writeErr != nil {
			return
		}

		select {
		case wsConnectedCh <- struct{}{}:
		default:
		}

		<-ctx.Done()
	}))

	return managerServer.URL, wsConnectedCh, managerServer.Close
}

func reserveTCPAddress(t *testing.T) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return lis.Addr().String(), func() {
		require.NoError(t, lis.Close())
	}
}

func startHangingTCPServer(t *testing.T, ctx context.Context) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := lis.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				<-ctx.Done()
			}()
		}
	}()

	stop := func() {
		_ = lis.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			require.FailNow(t, "timeout waiting for hanging TCP server shutdown")
		}
	}

	return lis.Addr().String(), stop
}

func startTestGRPCTunnelServerOnAPIPath(t *testing.T, ctx context.Context, tunnelServer *TunnelServer) (string, func()) {
	t.Helper()
	return startTestTunnelServiceOnAPIPath(t, ctx, tunnelServer)
}

func startTestTunnelServiceOnAPIPath(t *testing.T, ctx context.Context, service tunnelpb.TunnelServiceServer) (string, func()) {
	t.Helper()

	var serverOptions []grpc.ServerOption
	if tunnelServer, ok := service.(*TunnelServer); ok {
		serverOptions = tunnelServer.GRPCServerOptions()
	}
	grpcServer := grpc.NewServer(serverOptions...)
	tunnelpb.RegisterTunnelServiceServer(grpcServer, service)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clone := r.Clone(r.Context())
		if r.URL.Path == "/api/tunnel/connect" {
			clone.URL.Path = tunnelpb.TunnelService_Connect_FullMethodName
		} else {
			clone.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		}
		clone.RequestURI = clone.URL.Path
		grpcServer.ServeHTTP(w, clone)
	})

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	httpServer := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = httpServer.Serve(lis)
	}()

	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		grpcServer.Stop()
		_ = lis.Close()
	}

	return "http://" + lis.Addr().String(), cleanup
}

func TestTunnelClient_connectAndServeManagedTunnel_AutoDoesNotFallbackWhenEstablishedGRPCSessionDrops(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	envID := "env-auto-grpc-drop"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServerWithRegistry(GetRegistry(), resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	grpcManagerURL, stopGRPCManager := startTestGRPCTunnelServerOnAPIPath(t, ctx, tunnelServer)
	defer stopGRPCManager()

	wsManagerURL, wsConnectedCh, stopWSManager := startTestWebSocketTunnelManager(t, ctx)
	defer stopWSManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportAuto,
		ManagerApiUrl: wsManagerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	grpcAddr, err := url.Parse(grpcManagerURL)
	require.NoError(t, err)
	client.managerGRPCAddr = grpcAddr.Host
	client.healthySessionDuration = time.Millisecond

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServeManagedTunnel(ctx)
	}()

	var tunnel *AgentTunnel
	require.Eventually(t, func() bool {
		registered, ok := GetRegistry().Get(envID).Get()
		if !ok || registered == nil || registered.Conn == nil || registered.Conn.IsClosed() {
			return false
		}
		tunnel = registered
		return true
	}, 3*time.Second, 20*time.Millisecond)
	require.Equal(t, EdgeTransportGRPC, tunnel.Conn.Transport())

	time.Sleep(5 * time.Millisecond)
	require.NoError(t, tunnel.CloseWithReason("manager restart"))

	select {
	case operationErr := <-errCh:
		require.ErrorIs(t, operationErr, errEstablishedTunnelSessionEnded)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "expected dropped gRPC session to return instead of falling back to websocket")
	}

	select {
	case <-wsConnectedCh:
		require.FailNow(t, "dropped gRPC session must not fall back to websocket")
	default:
	}
	assert.Zero(t, client.grpcFailureStreak)
	cancel()
}

func TestTunnelClient_connectAndServePoll_OpensGRPCWhenRequired(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	envID := "env-poll-grpc"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	resolver := func(ctx context.Context, token string) (string, error) {
		if token != "valid-token" {
			return "", errors.New("invalid token")
		}
		return envID, nil
	}

	tunnelServer := NewTunnelServerWithRegistry(GetRegistry(), resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	managerURL, stopManager := startTestPollAndGRPCManager(t, ctx, tunnelServer, TunnelPollResponse{
		Status:              TunnelStatusRequired,
		PollIntervalSeconds: 1,
	})
	defer stopManager()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerURL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	errCh := make(chan error, 1)
	go func() {
		errCh <- client.connectAndServePoll(ctx)
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID).Get()
		if !ok || tunnel == nil || tunnel.Conn == nil || tunnel.Conn.IsClosed() {
			return false
		}
		_, isGRPC := tunnel.Conn.(*GRPCManagerTunnelConn)
		return isGRPC
	}, 3*time.Second, 20*time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "expected poll-managed gRPC session to stop after cancellation")
	}
}

func TestTunnelClient_connectAndServePoll_OpensWebSocketWhenRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	var pollCount atomic.Int32
	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			pollCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusRequired,
				PollIntervalSeconds: 1,
			})) {
				return
			}
		case "/api/tunnel/connect":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.CloseNow() }()

			if _, _, readErr := conn.Read(r.Context()); readErr != nil {
				return
			}
			registerResp, err := json.Marshal(&TunnelMessage{
				Type:          MessageTypeRegisterResponse,
				Accepted:      true,
				EnvironmentID: "env-poll-ws",
				SessionID:     "session-poll-ws",
				Capabilities:  []string{tunnelCapabilityMuxGrant},
			})
			if err != nil {
				return
			}
			if writeErr := conn.Write(r.Context(), websocket.MessageText, registerResp); writeErr != nil {
				return
			}

			select {
			case wsConnectedCh <- struct{}{}:
			default:
			}

			<-ctx.Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer managerServer.Close()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.registrationTimeout = 100 * time.Millisecond

	err := client.connectAndServePoll(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "expected poll transport to open websocket tunnel when required")
	}

	assert.GreaterOrEqual(t, pollCount.Load(), int32(1))
}

func TestTunnelClient_pollTunnelControl_UsesConfiguredHTTPClient(t *testing.T) {
	t.Parallel()

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !assert.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
			Status:              TunnelStatusIdle,
			PollIntervalSeconds: 1,
		})) {
			return
		}
	}))
	defer managerServer.Close()

	baseClient := managerServer.Client()
	rewriteTransport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = "http"
		clone.URL.Host = strings.TrimPrefix(managerServer.URL, "http://")
		return baseClient.Transport.RoundTrip(clone)
	})

	client := NewTunnelClient(&Config{AgentToken: "valid-token"}, http.NotFoundHandler())
	httpClient := &http.Client{Transport: rewriteTransport}

	resp, err := client.pollTunnelControl(t.Context(), httpClient, "http://127.0.0.1:1/api/tunnel/poll", false)
	require.NoError(t, err)
	assert.Equal(t, TunnelStatusIdle, resp.Status)
	assert.Equal(t, 1, resp.PollIntervalSeconds)
	assert.NotSame(t, http.DefaultClient, httpClient)

	defaultReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:1/api/tunnel/poll", http.NoBody)
	require.NoError(t, err)
	_, err = http.DefaultClient.Do(defaultReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1")
}

func TestTunnelClient_connectAndServePoll_DoesNotOpenWebSocketWhenIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusIdle,
				PollIntervalSeconds: 1,
			})) {
				return
			}
		case "/api/tunnel/connect":
			select {
			case wsConnectedCh <- struct{}{}:
			default:
			}
			http.Error(w, "unexpected websocket connect", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer managerServer.Close()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())

	err := client.connectAndServePoll(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
		require.FailNow(t, "did not expect idle poll transport to open websocket tunnel")
	default:
	}
}

func TestTunnelClient_connectAndServePoll_RetriesAfterTransientPollError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	var pollCount atomic.Int32
	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			currentPoll := pollCount.Add(1)
			if currentPoll == 1 {
				http.Error(w, "temporary failure", http.StatusBadGateway)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusRequired,
				PollIntervalSeconds: 1,
			})) {
				return
			}
		case "/api/tunnel/connect":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer func() { _ = conn.CloseNow() }()

			if _, _, readErr := conn.Read(r.Context()); readErr != nil {
				return
			}
			registerResp, err := json.Marshal(&TunnelMessage{
				Type:          MessageTypeRegisterResponse,
				Accepted:      true,
				EnvironmentID: "env-poll-retry-ws",
				SessionID:     "session-poll-retry-ws",
				Capabilities:  []string{tunnelCapabilityMuxGrant},
			})
			if err != nil {
				return
			}
			if writeErr := conn.Write(r.Context(), websocket.MessageText, registerResp); writeErr != nil {
				return
			}

			select {
			case wsConnectedCh <- struct{}{}:
			default:
			}

			<-ctx.Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer managerServer.Close()

	client := NewTunnelClient(&Config{
		EdgeTransport: EdgeTransportPoll,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "valid-token",
	}, http.NotFoundHandler())
	client.registrationTimeout = 100 * time.Millisecond

	err := client.connectAndServePoll(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "expected poll transport to recover after transient poll error")
	}

	assert.GreaterOrEqual(t, pollCount.Load(), int32(2))
}

func TestTunnelClient_stopPollManagedSession_DeadlineExceededReturnsTimeoutMessage(t *testing.T) {
	t.Parallel()

	session := &pollManagedTunnelSession{
		cancel: func() {},
		done:   make(chan error),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err := (&TunnelClient{}).stopPollManagedSession(ctx, session)
	require.Error(t, err)
	assert.EqualError(t, err, "timed out waiting for poll-managed websocket session to stop")
}

func TestTunnelClient_syncPollManagedSession_IdleUsesBoundedStopTimeout(t *testing.T) {
	previousTimeout := defaultPollManagedSessionStopTimeout
	defaultPollManagedSessionStopTimeout = 20 * time.Millisecond
	defer func() {
		defaultPollManagedSessionStopTimeout = previousTimeout
	}()

	session := &pollManagedTunnelSession{
		cancel: func() {},
		done:   make(chan error),
	}

	nextSession, err := (&TunnelClient{}).syncPollManagedSession(t.Context(), session, TunnelStatusIdle)
	require.Same(t, session, nextSession)
	require.Error(t, err)
	require.EqualError(t, err, "timed out waiting for poll-managed websocket session to stop")

	fencedSession, err := (&TunnelClient{}).syncPollManagedSession(t.Context(), nextSession, TunnelStatusRequired)
	require.NoError(t, err)
	require.Same(t, session, fencedSession)

	started := time.Now()
	stillDraining, err := (&TunnelClient{}).syncPollManagedSession(t.Context(), nextSession, TunnelStatusIdle)
	require.NoError(t, err)
	require.Same(t, session, stillDraining)
	require.Less(t, time.Since(started), defaultPollManagedSessionStopTimeout)
}

func startTestPollAndGRPCManager(t *testing.T, ctx context.Context, service tunnelpb.TunnelServiceServer, pollResp TunnelPollResponse) (string, func()) {
	t.Helper()

	var serverOptions []grpc.ServerOption
	if tunnelServer, ok := service.(*TunnelServer); ok {
		serverOptions = tunnelServer.GRPCServerOptions()
	}
	grpcServer := grpc.NewServer(serverOptions...)
	tunnelpb.RegisterTunnelServiceServer(grpcServer, service)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tunnel/poll" {
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode(pollResp)) {
				return
			}
			return
		}

		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clone := r.Clone(r.Context())
		if r.URL.Path == "/api/tunnel/connect" {
			clone.URL.Path = tunnelpb.TunnelService_Connect_FullMethodName
		} else {
			clone.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		}
		clone.RequestURI = clone.URL.Path
		grpcServer.ServeHTTP(w, clone)
	})

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	httpServer := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = httpServer.Serve(lis)
	}()

	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		grpcServer.Stop()
		_ = lis.Close()
	}

	return "http://" + lis.Addr().String(), cleanup
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	if f == nil {
		return nil, errors.New("round tripper is nil")
	}
	return f(req)
}

type fakeTunnelConn struct {
	mu         sync.Mutex
	closed     bool
	sendErr    error
	receiveErr error
	transport  string
}

func (f *fakeTunnelConn) Send(*TunnelMessage) error {
	return f.sendErr
}

func (f *fakeTunnelConn) Receive() (*TunnelMessage, error) {
	if f.receiveErr == nil {
		return nil, errors.New("not implemented")
	}
	return nil, f.receiveErr
}

func (f *fakeTunnelConn) muxConn(context.Context) io.ReadWriteCloser {
	return nil
}

func (f *fakeTunnelConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeTunnelConn) IsClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeTunnelConn) Transport() string {
	if f.transport == "" {
		return EdgeTransportWebSocket
	}
	return f.transport
}

func TestTunnelClient_serveTunnelSession_GRPCSendEOFSurfacesRegistrationError(t *testing.T) {
	conn := &fakeTunnelConn{
		sendErr:    io.EOF,
		receiveErr: status.Error(codes.Unauthenticated, "invalid agent token"),
		transport:  EdgeTransportGRPC,
	}
	client := NewTunnelClient(&Config{}, http.NotFoundHandler())

	err := client.serveTunnelSession(t.Context(), conn, "manager.test")
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Contains(t, err.Error(), "invalid agent token")
}

func TestConnectAndServeWebSocket_CancelUnblocksMessageLoop(t *testing.T) {
	registered := make(chan struct{})
	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}
		registerResp, _ := json.Marshal(&TunnelMessage{
			Type:         MessageTypeRegisterResponse,
			Accepted:     true,
			SessionID:    "session-cancel",
			Capabilities: []string{tunnelCapabilityMuxGrant},
		})
		_ = conn.Write(r.Context(), websocket.MessageText, registerResp)
		close(registered)
		// Go silent: keep reading so the socket stays open.
		for {
			if _, _, readErr := conn.Read(r.Context()); readErr != nil {
				return
			}
		}
	}))
	defer managerServer.Close()

	cfg := &Config{
		EdgeTransport: EdgeTransportWebSocket,
		ManagerApiUrl: managerServer.URL,
		AgentToken:    "test-token",
	}
	client := NewTunnelClient(cfg, http.NotFoundHandler())
	client.managerURL = "ws" + strings.TrimPrefix(managerServer.URL, "http")

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- client.connectAndServeWebSocket(ctx) }()

	select {
	case <-registered:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for registration")
	}
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		// Cancellation must close the socket and unblock the message loop well
		// under the poll-managed teardown timeout.
	case <-time.After(defaultPollManagedSessionStopTimeout):
		require.FailNow(t, "messageLoop did not unblock after context cancellation")
	}
}
