package agent

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTunnelTransport struct {
	ensureAvailable func(ctx context.Context, envID string) error
	do              func(ctx context.Context, envID, method, path string, headers http.Header, body []byte) (*RemoteResponse, error)
}

func (f fakeTunnelTransport) EnsureAvailable(ctx context.Context, envID string) error {
	return f.ensureAvailable(ctx, envID)
}

func (f fakeTunnelTransport) Do(ctx context.Context, envID, method, path string, headers http.Header, body []byte) (*RemoteResponse, error) {
	return f.do(ctx, envID, method, path, headers, body)
}

func TestRedactedTokenFingerprint(t *testing.T) {
	require.Equal(t, "***", RedactedTokenFingerprint(""))
	require.Equal(t, "***", RedactedTokenFingerprint("short"))
	require.Equal(t, "***", RedactedTokenFingerprint("1234567890"))
	require.Equal(t, "123456...cdef", RedactedTokenFingerprint(" 1234567890abcdef "))
}

func TestRemoteClientDo_UsesTunnelTransport(t *testing.T) {
	var ensured bool
	client := NewRemoteClient(RemoteClientConfig{
		Tunnel: fakeTunnelTransport{
			ensureAvailable: func(_ context.Context, envID string) error {
				ensured = true
				require.Equal(t, "env-agent-1", envID)
				return nil
			},
			do: func(_ context.Context, envID, method, path string, headers http.Header, _ []byte) (*RemoteResponse, error) {
				require.Equal(t, "env-agent-1", envID)
				require.Equal(t, http.MethodGet, method)
				require.Equal(t, "/api/health", path)
				require.Equal(t, "token", headers.Get(HeaderAgentToken))
				return &RemoteResponse{
					StatusCode: http.StatusOK,
					Body:       []byte(`{"agent":true}`),
					Headers:    map[string]string{"Content-Type": "application/json"},
				}, nil
			},
		},
	})

	resp, err := client.Do(t.Context(), RemoteRequest{
		EnvironmentID: "env-agent-1",
		Method:        http.MethodGet,
		Path:          "/api/health",
		Headers:       http.Header{HeaderAgentToken: {"token"}},
	})
	require.NoError(t, err)
	require.True(t, ensured)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, `{"agent":true}`, string(resp.Body))
}

func TestRemoteClientDo_DefaultTimeout(t *testing.T) {
	newClient := func(timeout time.Duration, deadlines chan<- time.Duration) *RemoteClient {
		return NewRemoteClient(RemoteClientConfig{
			Tunnel: fakeTunnelTransport{
				ensureAvailable: func(context.Context, string) error {
					return nil
				},
				do: func(ctx context.Context, _, _, _ string, _ http.Header, _ []byte) (*RemoteResponse, error) {
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					deadlines <- time.Until(deadline)
					return &RemoteResponse{StatusCode: http.StatusOK}, nil
				},
			},
			DefaultTimeout: timeout,
		})
	}
	deadlines := make(chan time.Duration, 1)

	_, err := newClient(0, deadlines).Do(t.Context(), RemoteRequest{EnvironmentID: "env"})
	require.NoError(t, err)
	assert.InDelta(t, (5 * time.Minute).Seconds(), (<-deadlines).Seconds(), 5)

	_, err = newClient(time.Minute, deadlines).Do(t.Context(), RemoteRequest{EnvironmentID: "env"})
	require.NoError(t, err)
	assert.InDelta(t, time.Minute.Seconds(), (<-deadlines).Seconds(), 5)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, err = newClient(time.Minute, deadlines).Do(ctx, RemoteRequest{EnvironmentID: "env"})
	require.NoError(t, err)
	assert.InDelta(t, (10 * time.Second).Seconds(), (<-deadlines).Seconds(), 5)
}

func TestRemoteClientDo_WrapsTransportErrors(t *testing.T) {
	client := NewRemoteClient(RemoteClientConfig{
		Tunnel: fakeTunnelTransport{
			ensureAvailable: func(context.Context, string) error {
				return ErrEnvironmentUnavailable
			},
		},
	})

	_, err := client.Do(t.Context(), RemoteRequest{
		EnvironmentID: "env-agent-2",
		Method:        http.MethodGet,
		Path:          "/api/health",
	})
	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Contains(t, transportErr.Error(), "not connected")
	require.True(t, transportErr.Unavailable())
	for _, test := range []struct {
		cause       error
		name        string
		unavailable bool
	}{
		{name: "deadline", cause: context.DeadlineExceeded, unavailable: true},
		{name: "response truncated", cause: io.ErrUnexpectedEOF},
		{name: "invalid request", cause: errors.New("invalid URL")},
		{name: "canceled", cause: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.unavailable, (&TransportError{Err: test.cause}).Unavailable())
		})
	}
}

func TestRemoteClient_AgentWithTunnel(t *testing.T) {
	server, tunnel := setupMockAgentServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent":true}`))
	})
	defer server.Close()
	defer func() { _ = tunnel.CloseWithReason("") }()

	envID := "env-agent-1"
	GetRegistry().Register(envID, tunnel)
	defer GetRegistry().Unregister(envID)

	client := NewRemoteClient(RemoteClientConfig{})

	resp, err := client.Do(t.Context(), RemoteRequest{
		EnvironmentID: envID,
		Method:        http.MethodGet,
		Path:          "/api/health",
		Headers:       http.Header{"X-H": {"v"}},
	})

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []byte(`{"agent":true}`), resp.Body)
}

func TestRemoteClient_AgentNoTunnel(t *testing.T) {
	client := NewRemoteClient(RemoteClientConfig{})

	_, err := client.Do(t.Context(), RemoteRequest{
		EnvironmentID: "env-agent-missing",
		Method:        http.MethodGet,
		Path:          "/api/health",
	})

	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	assert.Contains(t, err.Error(), "not connected")
}
