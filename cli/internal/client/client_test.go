package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/types"
)

func TestClient_UsesAPIKeyHeader(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-KEY"); got != "arc_test_key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Authorization"); got != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := New(cfg)

	require.NoError(t, err,
		"New() error: %v", err)

	resp, err := c.Get(t.Context(), "/api/version")

	require.NoError(t, err,
		"Get() error: %v", err)

	_ = resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status: %d", resp.StatusCode)
}

func TestClient_UsesBearerTokenHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer header.payload.sig" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("X-API-KEY"); got != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	for _, source := range []string{"config", "environment"} {
		t.Run(source, func(t *testing.T) {
			cfg := &types.Config{ServerURL: srv.URL, JWTToken: "header.payload.sig"}
			if source == "environment" {
				t.Setenv("ARCANE_TOKEN", "header.payload.sig")
				cfg.APIKey, cfg.JWTToken, cfg.RefreshToken = "arc_test_key", "", "saved-refresh"
			}
			c, err := New(cfg)

			require.NoError(t, err,
				"New() error: %v", err)

			resp, err := c.Get(t.Context(), "/api/version")

			require.NoError(t, err,
				"Get() error: %v", err)

			_ = resp.Body.Close()

			require.Equal(t, http.StatusOK, resp.StatusCode,
				"unexpected status: %d", resp.StatusCode)
			if source == "environment" {
				require.Equal(t, "saved-refresh", cfg.RefreshToken)
				require.Empty(t, c.refreshToken)
			}
		})
	}
}

func TestClient_NewUnauthenticated_DoesNotSendAuthHeaders(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-API-KEY") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL}
	c, err := NewUnauthenticated(cfg)

	require.NoError(t, err,
		"NewUnauthenticated() error: %v", err)

	resp, err := c.Get(t.Context(), "/api/auth/login")

	require.NoError(t, err,
		"Get() error: %v", err)

	_ = resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status: %d", resp.StatusCode)
}

func TestClient_Request_DoesNotDoubleMarshalBytes(t *testing.T) {
	t.Parallel()

	type payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p payload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if p.Username != "arcane" || p.Password != "secret" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, JWTToken: "header.payload.sig"}
	c, err := New(cfg)

	require.NoError(t, err,
		"New() error: %v", err)

	raw, err := json.Marshal(payload{Username: "arcane", Password: "secret"})

	require.NoError(t, err,
		"json.Marshal() error: %v", err)

	resp, err := c.Post(t.Context(), "/api/auth/login", raw)

	require.NoError(t, err,
		"Post() error: %v", err)

	_ = resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode,
		"unexpected status: %d", resp.StatusCode)
}

func TestClient_RetriesIdempotentRequests(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"value":"ok"}}`))
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := New(cfg)

	require.NoError(t, err,
		"New() error: %v", err)

	c.SetRetryPolicy(3, 1*time.Millisecond, 2*time.Millisecond)

	resp, err := c.Get(t.Context(), "/api/version")

	require.NoError(t, err,
		"Get() error: %v", err)

	_ = resp.Body.Close()
	{
		got := attempts.Load()
		require.Equal(t, int32(2), got,
			"expected 2 attempts, got %d", got)
	}
}

func TestClient_DoesNotRetryNonIdempotentRequests(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"temporary"}`))
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := New(cfg)

	require.NoError(t, err,
		"New() error: %v", err)

	c.SetRetryPolicy(3, 1*time.Millisecond, 2*time.Millisecond)

	resp, err := c.Post(t.Context(), "/api/version", map[string]any{"a": 1})

	require.NoError(t, err,
		"Post() returned transport error: %v", err)

	_ = resp.Body.Close()
	{
		got := attempts.Load()
		require.Equal(t, int32(1), got,
			"expected 1 attempt, got %d", got)
	}
}

func TestClient_DoJSON_StrictStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := New(cfg)

	require.NoError(t, err,
		"New() error: %v", err)

	{
		_, requestErr := c.DoJSON[map[string]any](t.Context(), http.MethodGet, "/api/version", nil)
		require.Error(t, requestErr,
			"expected strict status error")
	}
}

func TestDecodeResponseStrict_RequiresSuccessEnvelope(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rec.Code = http.StatusOK
	rec.Body.WriteString(`{"success":false,"error":"not ok"}`)

	resp := rec.Result()
	{
		_, err := decodeResponseStrict[map[string]any](resp)
		require.Error(t, err,
			"expected envelope failure")
	}
}

func TestClient_DoRaw_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := New(cfg)

	require.NoError(t, err,
		"New() error: %v", err)

	b, err := c.DoRaw(t.Context(), http.MethodGet, "/api/version", nil)

	require.NoError(t, err,
		"DoRaw() error: %v", err)

	require.NotEmpty(t, b,
		"expected response payload")
}

func TestClientDoEnvelope(t *testing.T) {
	type record struct {
		Name string `json:"name"`
	}
	for _, tt := range []struct {
		name, method, response, wantErr string
		status                          int
		body                            any
	}{
		{name: "GET", method: http.MethodGet, status: http.StatusOK, response: `{"success":true,"data":{"name":"arcane"}}`},
		{
			name: "POST body", method: http.MethodPost, body: map[string]string{"name": "arcane"}, status: http.StatusCreated,
			response: `{"success":true,"data":{"name":"arcane"}}`,
		},
		{
			name: "PUT body", method: http.MethodPut, body: map[string]string{"name": "arcane"}, status: http.StatusOK,
			response: `{"success":true,"data":{"name":"arcane"}}`,
		},
		{
			name: "DELETE body", method: http.MethodDelete, body: map[string]string{"name": "arcane"}, status: http.StatusOK,
			response: `{"success":true,"data":{"name":"arcane"}}`,
		},
		{name: "HTTP failure", method: http.MethodGet, status: http.StatusForbidden, response: `{"error":"denied"}`, wantErr: "request failed with status 403"},
		{name: "API failure", method: http.MethodGet, status: http.StatusOK, response: `{"success":false,"error":"denied"}`, wantErr: "API error: denied"},
		{name: "failure without message", method: http.MethodGet, status: http.StatusOK, response: `{"success":false}`, wantErr: "API error: request was not successful"},
		{name: "missing success", method: http.MethodGet, status: http.StatusOK, response: `{"data":{"name":"arcane"}}`, wantErr: "API error: request was not successful"},
		{name: "invalid JSON", method: http.MethodGet, status: http.StatusOK, response: `invalid`, wantErr: "failed to decode response"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestBody, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- r.Method + " " + r.URL.RequestURI() + " " + string(requestBody)
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprint(w, tt.response)
			}))
			defer server.Close()
			c, newErr := NewUnauthenticated(&types.Config{ServerURL: server.URL})
			require.NoError(t, newErr)
			result, requestErr := c.DoEnvelope[record](t.Context(), tt.method, "/api/record?full=true", tt.body)
			if tt.wantErr != "" {
				require.ErrorContains(t, requestErr, tt.wantErr)
			} else {
				require.NoError(t, requestErr)
				require.True(t, result.Success)
				require.Equal(t, record{Name: "arcane"}, result.Data)
			}
			request := <-requests
			require.True(t, strings.HasPrefix(request, tt.method+" /api/record?full=true "))
			if tt.body != nil {
				require.Contains(t, request, `{"name":"arcane"}`)
			}
		})
	}
}
