package remenv

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestClientDo_DirectHTTPSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/registries/sync", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)
	resp, err := client.Do(context.Background(), Request{
		Method:  http.MethodPost,
		URL:     server.URL + "/api/registries/sync",
		Path:    "/api/registries/sync",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(`{"sync":true}`),
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Equal(t, `{"ok":true}`, string(resp.Body))
	require.Equal(t, "application/json", resp.Headers["Content-Type"])
}

func TestClientDo_EdgeUsesTunnelTransport(t *testing.T) {
	var ensured bool
	client := NewClient(nil, TunnelTransportFuncs{
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

	resp, err := client.Do(context.Background(), Request{
		EnvironmentID: "env-edge-1",
		IsEdge:        true,
		Method:        http.MethodGet,
		Path:          "/api/health",
	})
	require.NoError(t, err)
	require.True(t, ensured)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, `{"edge":true}`, string(resp.Body))
}

func TestClientDoJSON_ClassifiesStatusAndDecodeErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			http.Error(w, "bad gateway", http.StatusBadGateway)
		case "/decode":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"broken"`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client(), nil)

	var statusOut map[string]any
	err := client.DoJSON(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL + "/status",
		Path:   "/status",
	}, &statusOut)
	var statusErr *StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusBadGateway, statusErr.StatusCode)

	var decodeOut map[string]any
	err = client.DoJSON(context.Background(), Request{
		Method: http.MethodGet,
		URL:    server.URL + "/decode",
		Path:   "/decode",
	}, &decodeOut)
	var decodeErr *DecodeError
	require.ErrorAs(t, err, &decodeErr)
}

func TestClientDo_WrapsTransportErrors(t *testing.T) {
	client := NewClient(nil, TunnelTransportFuncs{
		EnsureAvailableFunc: func(ctx context.Context, envID string) error {
			return errors.New("not connected")
		},
	})

	_, err := client.Do(context.Background(), Request{
		EnvironmentID: "env-edge-2",
		IsEdge:        true,
		Method:        http.MethodGet,
		Path:          "/api/health",
	})
	var transportErr *TransportError
	require.ErrorAs(t, err, &transportErr)
	require.Contains(t, transportErr.Error(), "not connected")
}
