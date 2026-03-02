package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/cli/internal/types"
)

func TestClient_RetriesIdempotentRequests(t *testing.T) {
	t.Parallel()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
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
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	c.SetRetryPolicy(3, 1*time.Millisecond, 2*time.Millisecond)

	resp, err := c.Get(context.Background(), "/api/version")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	_ = resp.Body.Close()
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 attempts, got %d", got)
	}
}

func TestClient_DoesNotRetryNonIdempotentRequests(t *testing.T) {
	t.Parallel()

	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"temporary"}`))
	}))
	defer srv.Close()

	cfg := &types.Config{ServerURL: srv.URL, APIKey: "arc_test_key"}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	c.SetRetryPolicy(3, 1*time.Millisecond, 2*time.Millisecond)

	resp, err := c.Post(context.Background(), "/api/version", map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("Post() returned transport error: %v", err)
	}
	_ = resp.Body.Close()
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected 1 attempt, got %d", got)
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
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	var out map[string]any
	if err := c.DoJSON(context.Background(), http.MethodGet, "/api/version", nil, &out); err == nil {
		t.Fatal("expected strict status error")
	}
}

func TestDecodeResponseStrict_RequiresSuccessEnvelope(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	rec.Code = http.StatusOK
	rec.Body.WriteString(`{"success":false,"error":"not ok"}`)

	resp := rec.Result()
	if _, err := DecodeResponseStrict[map[string]any](resp); err == nil {
		t.Fatal("expected envelope failure")
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
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	b, err := c.DoRaw(context.Background(), http.MethodGet, "/api/version", nil)
	if err != nil {
		t.Fatalf("DoRaw() error: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected response payload")
	}
}
