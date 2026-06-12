package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
)

func TestBuildHealthURLInternalDefaults(t *testing.T) {
	t.Run("default_port", func(t *testing.T) {
		cfg := &config.Config{}
		healthURL, err := buildHealthURLInternal(cfg)
		if err != nil {
			t.Fatalf("build health URL failed: %v", err)
		}

		if !strings.HasPrefix(healthURL, "http://127.0.0.1:3552") {
			t.Fatalf("unexpected health URL: %s", healthURL)
		}
		if !strings.HasSuffix(healthURL, "/api/health") {
			t.Fatalf("expected health URL path /api/health, got: %s", healthURL)
		}
	})

	t.Run("explicit_port", func(t *testing.T) {
		cfg := &config.Config{Port: "8443"}
		healthURL, err := buildHealthURLInternal(cfg)
		if err != nil {
			t.Fatalf("build health URL failed: %v", err)
		}
		if !strings.HasPrefix(healthURL, "http://127.0.0.1:8443") {
			t.Fatalf("unexpected health URL: %s", healthURL)
		}
	})
}

func TestBuildHealthURLInternalInvalidPort(t *testing.T) {
	cfg := &config.Config{Port: "invalid-port"}
	_, err := buildHealthURLInternal(cfg)
	if err == nil {
		t.Fatal("expected invalid health URL port to fail")
	}
}

func TestRunHealthCommandInternal(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("expected HEAD request, got: %s", r.Method)
		}
		if r.URL.Path != "/api/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	_, port, ok := strings.Cut(strings.TrimPrefix(s.URL, "http://"), ":")
	if !ok {
		t.Fatalf("failed to parse port from %s", s.URL)
	}
	if strings.Contains(port, "/") {
		port = strings.SplitN(port, "/", 2)[0]
	}
	if _, err := strconv.Atoi(port); err != nil {
		t.Fatalf("invalid port in test server URL %q: %v", s.URL, err)
	}

	cfg := &config.Config{Port: port}
	if err := runHealthCommandInternal(context.Background(), cfg, 2*time.Second); err != nil {
		t.Fatalf("health command failed: %v", err)
	}
}

func TestRunHealthCommandInternalNon2xx(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer s.Close()

	_, port, ok := strings.Cut(strings.TrimPrefix(s.URL, "http://"), ":")
	if !ok {
		t.Fatalf("failed to parse port from %s", s.URL)
	}
	if strings.Contains(port, "/") {
		port = strings.SplitN(port, "/", 2)[0]
	}

	cfg := &config.Config{Port: port}
	err := runHealthCommandInternal(context.Background(), cfg, 2*time.Second)
	if err == nil {
		t.Fatal("expected non-2xx response to fail")
	}
	if !strings.Contains(err.Error(), "health check failed with status") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunHealthCommandInternalConnectionFailure(t *testing.T) {
	err := runHealthCommandInternal(context.Background(), &config.Config{Port: "1"}, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected connection failure")
	}
}

func TestRunHealthCommandInternalDefaultTimeoutFallback(t *testing.T) {
	err := runHealthCommandInternal(context.Background(), &config.Config{Port: "1"}, 0)
	if err == nil {
		t.Fatal("expected connection failure")
	}
	if !strings.Contains(err.Error(), "health check request failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ensure timeout path is exercised by checking the string formatting path.
	if !strings.Contains(fmt.Sprint(defaultHealthTimeout), "5s") {
		t.Fatalf("unexpected default timeout: %s", defaultHealthTimeout)
	}
}
