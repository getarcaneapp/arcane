package edge

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	tunnelpb "github.com/getarcaneapp/arcane/backend/pkg/libarcane/edge/proto/tunnel/v1"
	"github.com/getarcaneapp/arcane/backend/pkg/remenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func newTestRemenvClientInternal(timeout time.Duration) *remenv.Client {
	return remenv.NewClient(&http.Client{Timeout: timeout}, remenv.TunnelTransportFuncs{
		EnsureAvailableFunc: func(ctx context.Context, envID string) error {
			if HasActiveTunnel(envID) {
				return nil
			}

			if _, ok := RequestTunnelAndWait(ctx, envID, DefaultTunnelDemandTTL, DefaultTunnelAcquireTimeout()); ok {
				return nil
			}

			return fmt.Errorf("edge agent is not connected (no active tunnel)")
		},
		DoFunc: func(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*remenv.Response, error) {
			tunnel, ok := GetRegistry().Get(envID)
			if !ok {
				return nil, fmt.Errorf("no active tunnel for environment %s", envID)
			}
			if tunnel.Conn.IsClosed() {
				return nil, fmt.Errorf("tunnel for environment %s is closed", envID)
			}

			statusCode, respHeaders, respBody, err := ProxyRequest(ctx, tunnel, method, path, "", headers, body)
			if err != nil {
				return nil, fmt.Errorf("tunnel request failed: %w", err)
			}

			return &remenv.Response{
				StatusCode: statusCode,
				Body:       respBody,
				Headers:    respHeaders,
			}, nil
		},
	})
}

func TestRemenvClient_EdgeWithTunnel(t *testing.T) {
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

	client := newTestRemenvClientInternal(1 * time.Second)

	resp, err := client.Do(context.Background(), remenv.Request{
		EnvironmentID: envID,
		IsEdge:        true,
		Method:        http.MethodGet,
		URL:           "http://ignored/api/health",
		Path:          "/api/health",
		Headers:       map[string]string{"X-H": "v"},
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte(`{"edge":true}`), resp.Body)
}

func TestRemenvClient_EdgeNoTunnel(t *testing.T) {
	client := newTestRemenvClientInternal(1 * time.Second)

	_, err := client.Do(context.Background(), remenv.Request{
		EnvironmentID: "env-edge-missing",
		IsEdge:        true,
		Method:        http.MethodGet,
		URL:           "http://ignored/api/health",
		Path:          "/api/health",
	})

	var transportErr *remenv.TransportError
	require.ErrorAs(t, err, &transportErr)
	assert.Contains(t, err.Error(), "not connected")
}

func TestRemenvClient_EdgeWithGRPCTunnel(t *testing.T) {
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

		req := msg.GetCommandRequest()
		if req == nil {
			agentErrCh <- errors.New("expected command request")
			return
		}

		if req.GetMethod() != http.MethodPost {
			agentErrCh <- errors.New("unexpected method")
			return
		}
		if req.GetPath() != "/api/environments/0/projects" {
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

		if err := stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandAck{CommandAck: &tunnelpb.CommandAck{
			CommandId: req.GetCommandId(),
		}}}); err != nil {
			agentErrCh <- err
			return
		}

		agentErrCh <- stream.Send(&tunnelpb.AgentMessage{Payload: &tunnelpb.AgentMessage_CommandComplete{CommandComplete: &tunnelpb.CommandComplete{
			CommandId: req.GetCommandId(),
			Status:    http.StatusCreated,
			Headers:   map[string]string{"Content-Type": "application/json"},
			Body:      []byte(`{"ok":true}`),
		}}})
	}()

	require.Eventually(t, func() bool {
		tunnel, ok := GetRegistry().Get(envID)
		return ok && tunnel != nil && !tunnel.Conn.IsClosed()
	}, time.Second, 10*time.Millisecond)

	client := newTestRemenvClientInternal(1 * time.Second)
	resp, err := client.Do(ctx, remenv.Request{
		EnvironmentID: envID,
		IsEdge:        true,
		Method:        http.MethodPost,
		URL:           "http://ignored/api/environments/0/projects",
		Path:          "/api/environments/0/projects",
		Headers:       map[string]string{"X-H": "v", "Content-Type": "application/json"},
		Body:          []byte(`{"edge":true}`),
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Headers["Content-Type"])
	assert.Equal(t, []byte(`{"ok":true}`), resp.Body)

	require.NoError(t, <-agentErrCh)
}
