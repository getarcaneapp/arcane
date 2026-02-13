package edge

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestEdgeAwareClient_DoForEnvironment_EdgeWithTunnel(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(msg *TunnelMessage) *TunnelMessage {
		return &TunnelMessage{
			ID:      msg.ID,
			Type:    MessageTypeResponse,
			Status:  http.StatusOK,
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"edge":true}`),
		}
	})
	defer server.Close()
	defer func() { _ = tunnel.Close() }()

	envID := "env-edge-1"
	GetRegistry().Register(envID, tunnel)
	defer GetRegistry().Unregister(envID)

	client := NewEdgeAwareClient(1 * time.Second)

	resp, err := client.DoForEnvironment(
		context.Background(),
		envID,
		true, // isEdge
		"GET",
		"http://ignored/api/path",
		"/api/path",
		map[string]string{"X-H": "v"},
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte(`{"edge":true}`), resp.Body)
}

func TestEdgeAwareClient_DoForEnvironment_EdgeNoTunnel(t *testing.T) {
	client := NewEdgeAwareClient(1 * time.Second)

	_, err := client.DoForEnvironment(
		context.Background(),
		"env-edge-missing",
		true, // isEdge
		"GET",
		"http://ignored/api/path",
		"/api/path",
		nil,
		nil,
	)

	assert.Contains(t, err.Error(), "not connected")
}

func TestEdgeAwareClient_DoForEnvironment_NonEdge(t *testing.T) {
	// Setup direct server
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/direct", r.URL.Path)
		assert.Equal(t, "val", r.Header.Get("X-Direct"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("direct response"))
	}))
	defer directServer.Close()

	client := NewEdgeAwareClient(1 * time.Second)

	resp, err := client.DoForEnvironment(
		context.Background(),
		"env-direct",
		false, // isEdge (false)
		"GET",
		directServer.URL+"/api/direct",
		"/api/direct",
		map[string]string{"X-Direct": "val"},
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	assert.Equal(t, []byte("direct response"), resp.Body)
}

func TestDoEdgeAwareRequest_Helper(t *testing.T) {
	// Just test it calls the default client correctly (using non-edge for simplicity)
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("helper"))
	}))
	defer directServer.Close()

	resp, err := DoEdgeAwareRequest(
		context.Background(),
		"env-helper",
		false,
		"GET",
		directServer.URL,
		"/",
		nil,
		nil,
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte("helper"), resp.Body)
}

func TestEdgeAwareClient_DoForEnvironment_EdgeWithGRPCTunnel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	envID := "env-edge-grpc-1"
	GetRegistry().Unregister(envID)
	defer GetRegistry().Unregister(envID)

	lis, grpcServer, tunnelServer := startTestGRPCTunnelServer(ctx, envID)
	defer func() {
		cancel()
		tunnelServer.WaitForCleanupDone()
	}()

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	defer grpcServer.GracefulStop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	clientAPI := tunnelpb.NewTunnelServiceClient(conn)
	stream, err := clientAPI.Connect(ctx)
	require.NoError(t, err)

	err = stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_Register{Register: &tunnelpb.RegisterRequest{AgentToken: "valid-token"}}})
	require.NoError(t, err)

	registerResp, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, registerResp.GetRegisterResponse())
	assert.True(t, registerResp.GetRegisterResponse().GetAccepted())

	agentErrCh := make(chan error, 1)
	go func() {
		msg, err := stream.Recv()
		if err != nil {
			agentErrCh <- err
			return
		}

		req := msg.GetHttpRequest()
		if req == nil {
			agentErrCh <- errors.New("expected http request")
			return
		}

		if req.GetMethod() != http.MethodPost {
			agentErrCh <- errors.New("unexpected method")
			return
		}
		if req.GetPath() != "/api/path" {
			agentErrCh <- errors.New("unexpected path")
			return
		}
		if req.GetHeaders()["X-H"] != "v" {
			agentErrCh <- errors.New("missing X-H header")
			return
		}
		if req.GetHeaders()["Content-Type"] != "application/json" {
			agentErrCh <- errors.New("missing content type")
			return
		}
		if string(req.GetBody()) != `{"edge":true}` {
			agentErrCh <- errors.New("unexpected request body")
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_HttpResponse{HttpResponse: &tunnelpb.HttpResponse{
			RequestId: req.GetRequestId(),
			Status:    http.StatusCreated,
			Headers:   map[string]string{"Content-Type": "application/json"},
			Body:      []byte(`{"ok":true}`),
		}}})
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, time.Second, 10*time.Millisecond)

	client := NewEdgeAwareClient(1 * time.Second)
	resp, err := client.DoForEnvironment(
		ctx,
		envID,
		true,
		http.MethodPost,
		"http://ignored/api/path",
		"/api/path",
		map[string]string{"X-H": "v", "Content-Type": "application/json"},
		[]byte(`{"edge":true}`),
	)

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, []byte(`{"ok":true}`), resp.Body)

	require.NoError(t, <-agentErrCh)
}
