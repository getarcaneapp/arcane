package edge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

func TestTunnelClient_connectAndServePoll_OpensGRPCWhenRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
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

	tunnelServer := NewTunnelServer(resolver, nil)
	go tunnelServer.StartCleanupLoop(ctx)
	defer tunnelServer.WaitForCleanupDone()

	managerURL, stopManager := startTestPollAndGRPCManagerInternal(t, ctx, tunnelServer, TunnelPollResponse{
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
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestTunnelClient_connectAndServePoll_OpensWebSocketWhenRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	var pollCount atomic.Int32
	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			pollCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusRequired,
				PollIntervalSeconds: 1,
			}))
		case "/api/tunnel/connect":
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

	err := client.connectAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected poll transport to open websocket tunnel when required")
	}

	assert.GreaterOrEqual(t, pollCount.Load(), int32(1))
}

func TestTunnelClient_pollTunnelControlInternal_UsesConfiguredHTTPClient(t *testing.T) {
	t.Parallel()

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
			Status:              TunnelStatusIdle,
			PollIntervalSeconds: 1,
		}))
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
	client.httpClient = &http.Client{Transport: rewriteTransport}

	resp, err := client.pollTunnelControlInternal(context.Background(), "http://127.0.0.1:1/api/tunnel/poll", false)
	require.NoError(t, err)
	assert.Equal(t, TunnelStatusIdle, resp.Status)
	assert.Equal(t, 1, resp.PollIntervalSeconds)
	assert.NotNil(t, client.httpClient)
	assert.NotSame(t, http.DefaultClient, client.httpClient)

	defaultReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://127.0.0.1:1/api/tunnel/poll", nil)
	require.NoError(t, err)
	_, err = http.DefaultClient.Do(defaultReq)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1:1")
}

func TestTunnelClient_connectAndServePoll_DoesNotOpenWebSocketWhenIdle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	wsConnectedCh := make(chan struct{}, 1)

	managerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tunnel/poll":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusIdle,
				PollIntervalSeconds: 1,
			}))
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

	err := client.connectAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
		t.Fatal("did not expect idle poll transport to open websocket tunnel")
	default:
	}
}

func TestTunnelClient_connectAndServePoll_RetriesAfterTransientPollError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
			require.NoError(t, json.NewEncoder(w).Encode(TunnelPollResponse{
				Status:              TunnelStatusRequired,
				PollIntervalSeconds: 1,
			}))
		case "/api/tunnel/connect":
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

	err := client.connectAndServe(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	select {
	case <-wsConnectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("expected poll transport to recover after transient poll error")
	}

	assert.GreaterOrEqual(t, pollCount.Load(), int32(2))
}

func TestTunnelClient_stopPollManagedSessionInternal_DeadlineExceededReturnsTimeoutMessage(t *testing.T) {
	t.Parallel()

	session := &pollManagedTunnelSession{
		cancel: func() {},
		done:   make(chan error),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := (&TunnelClient{}).stopPollManagedSessionInternal(ctx, session)
	require.Error(t, err)
	assert.EqualError(t, err, "timed out waiting for poll-managed websocket session to stop")
}

func TestTunnelClient_syncPollManagedSessionInternal_IdleUsesBoundedStopTimeout(t *testing.T) {
	previousTimeout := defaultPollManagedSessionStopTimeout
	defaultPollManagedSessionStopTimeout = 20 * time.Millisecond
	defer func() {
		defaultPollManagedSessionStopTimeout = previousTimeout
	}()

	session := &pollManagedTunnelSession{
		cancel: func() {},
		done:   make(chan error),
	}

	nextSession, err := (&TunnelClient{}).syncPollManagedSessionInternal(context.Background(), session, TunnelStatusIdle)
	require.Nil(t, nextSession)
	require.Error(t, err)
	assert.EqualError(t, err, "timed out waiting for poll-managed websocket session to stop")
}

func startTestPollAndGRPCManagerInternal(t *testing.T, ctx context.Context, service tunnelpb.TunnelServiceServer, pollResp TunnelPollResponse) (string, func()) {
	t.Helper()

	grpcServer := grpc.NewServer()
	tunnelpb.RegisterTunnelServiceServer(grpcServer, service)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	handler := h2c.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tunnel/poll" {
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(pollResp))
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	if f == nil {
		return nil, fmt.Errorf("round tripper is nil")
	}
	return f(req)
}
