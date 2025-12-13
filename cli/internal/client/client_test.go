package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.getarcane.app/cli/internal/types"
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
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	resp, err := c.Get(context.Background(), "/api/version")
	if err != nil {

		t.Fatalf("Get() error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestClient_UsesBearerTokenHeader(t *testing.T) {
	t.Parallel()

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

	cfg := &types.Config{ServerURL: srv.URL, JWTToken: "header.payload.sig"}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	resp, err := c.Get(context.Background(), "/api/version")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
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
	if err != nil {
		t.Fatalf("NewUnauthenticated() error: %v", err)
	}

	resp, err := c.Get(context.Background(), "/api/auth/login")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
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
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	raw, err := json.Marshal(payload{Username: "arcane", Password: "secret"})
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	resp, err := c.Post(context.Background(), "/api/auth/login", raw)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}
