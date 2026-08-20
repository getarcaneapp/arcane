package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/cli/v2/internal/types"
	"github.com/stretchr/testify/require"
)

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

	resp, err := c.Get(context.Background(), "/api/version")

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

	resp, err := c.Post(context.Background(), "/api/version", map[string]any{"a": 1})

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
		_, err := c.DoJSON[map[string]any](context.Background(), http.MethodGet, "/api/version", nil)
		require.Error(t, err,
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
		_, err := decodeResponseStrictInternal[map[string]any](resp)
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

	b, err := c.DoRaw(context.Background(), http.MethodGet, "/api/version", nil)

	require.NoError(t, err,
		"DoRaw() error: %v", err)

	require.NotEmpty(t, b,
		"expected response payload")

}
