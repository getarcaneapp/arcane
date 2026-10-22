package remenv

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyAgentTokenHeaders(t *testing.T) {
	headers := http.Header{}
	ApplyAgentTokenHeaders(headers, nil)
	require.Empty(t, headers.Get(HeaderAPIKey))
	require.Empty(t, headers.Get(HeaderAgentToken))

	token := "token-123"
	ApplyAgentTokenHeaders(headers, &token)
	require.Equal(t, token, headers.Get(HeaderAPIKey))
	require.Equal(t, token, headers.Get(HeaderAgentToken))

	headerMap := map[string]string{}
	ApplyAgentTokenHeaderMap(headerMap, &token)
	require.Equal(t, token, headerMap[HeaderAPIKey])
	require.Equal(t, token, headerMap[HeaderAgentToken])
}

func TestRedactedTokenFingerprint(t *testing.T) {
	require.Equal(t, "***", RedactedTokenFingerprint(""))
	require.Equal(t, "***", RedactedTokenFingerprint("short"))
	require.Equal(t, "***", RedactedTokenFingerprint("1234567890"))
	require.Equal(t, "123456...cdef", RedactedTokenFingerprint(" 1234567890abcdef "))
}

func TestClientDo_EdgeUsesTunnelTransport(t *testing.T) {
	var ensured bool
	client := NewClient(TunnelTransportFuncs{
		EnsureAvailableFunc: func(ctx context.Context, envID string) error {
			ensured = true
			require.Equal(t, "env-edge-1", envID)
			return nil
		},
		DoFunc: func(ctx context.Context, envID, method, path string, headers map[string]string, body []byte) (*Response, error) {
			require.Equal(t, "env-edge-1", envID)
			require.Equal(t, http.MethodGet, method)
			require.Equal(t, "/api/health", path)
			return &Response{
				StatusCode: http.StatusOK,
				Body:       []byte(`{"edge":true}`),
				Headers:    map[string]string{"Content-Type": "application/json"},
			}, nil
		},
	})

	resp, err := client.Do(t.Context(), Request{
		EnvironmentID: "env-edge-1",
		Method:        http.MethodGet,
		Path:          "/api/health",
	})
	require.NoError(t, err)
	require.True(t, ensured)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, `{"edge":true}`, string(resp.Body))
}

func TestClientDo_WrapsTransportErrors(t *testing.T) {
	client := NewClient(TunnelTransportFuncs{
		EnsureAvailableFunc: func(ctx context.Context, envID string) error {
			return ErrEnvironmentUnavailable
		},
	})

	_, err := client.Do(t.Context(), Request{
		EnvironmentID: "env-edge-2",
		Method:        http.MethodGet,
		Path:          "/api/health",
	})
	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Contains(t, transportErr.Error(), "not connected")
	require.True(t, transportErr.Unavailable())
	for _, test := range []struct {
		name        string
		cause       error
		unavailable bool
	}{
		{"deadline", context.DeadlineExceeded, true},
		{"response truncated", io.ErrUnexpectedEOF, false},
		{"invalid request", errors.New("invalid URL"), false},
		{"canceled", context.Canceled, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.unavailable, (&TransportError{Err: test.cause}).Unavailable())
		})
	}
}
