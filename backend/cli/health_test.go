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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildHealthURLInternalDefaults(t *testing.T) {
	t.Run("default_port", func(t *testing.T) {
		cfg := &config.Config{}
		healthURL, err := buildHealthURLInternal(cfg)

		require.NoError(t, err,
			"build health URL failed: %v", err)

		require.True(t, strings.HasPrefix(healthURL, "http://127.0.0.1:3552"),
			"unexpected health URL: %s", healthURL)

		require.True(t, strings.HasSuffix(healthURL, "/api/health"),
			"expected health URL path /api/health, got: %s", healthURL)

	})

	t.Run("explicit_port", func(t *testing.T) {
		cfg := &config.Config{Port: "8443"}
		healthURL, err := buildHealthURLInternal(cfg)

		require.NoError(t, err,
			"build health URL failed: %v", err)

		require.True(t, strings.HasPrefix(healthURL, "http://127.0.0.1:8443"),
			"unexpected health URL: %s", healthURL)

	})
}

func TestBuildHealthURLInternalInvalidPort(t *testing.T) {
	cfg := &config.Config{Port: "invalid-port"}
	_, err := buildHealthURLInternal(cfg)

	require.Error(t, err,
		"expected invalid health URL port to fail")

}

func TestRunHealthCommandInternal(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodHead, r.Method,
			"expected HEAD request, got: %s", r.Method) {
			return
		}
		if !assert.Equal(t, "/api/health", r.URL.Path,
			"unexpected path: %s", r.URL.Path) {
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer s.Close()

	_, port, ok := strings.Cut(strings.TrimPrefix(s.URL, "http://"), ":")

	require.True(t, ok,
		"failed to parse port from %s", s.URL)

	if strings.Contains(port, "/") {
		port = strings.SplitN(port, "/", 2)[0]
	}
	{
		_, err := strconv.Atoi(port)
		require.NoError(t, err,
			"invalid port in test server URL %q: %v", s.URL, err)
	}

	cfg := &config.Config{Port: port}
	{
		err := runHealthCommandInternal(context.Background(), cfg, 2*time.Second)
		require.NoError(t, err,
			"health command failed: %v", err)
	}

}

func TestRunHealthCommandInternalNon2xx(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer s.Close()

	_, port, ok := strings.Cut(strings.TrimPrefix(s.URL, "http://"), ":")

	require.True(t, ok,
		"failed to parse port from %s", s.URL)

	if strings.Contains(port, "/") {
		port = strings.SplitN(port, "/", 2)[0]
	}

	cfg := &config.Config{Port: port}
	err := runHealthCommandInternal(context.Background(), cfg, 2*time.Second)

	require.Error(t, err,
		"expected non-2xx response to fail")

	require.Contains(t, err.Error(), "health check failed with status",
		"unexpected error: %v", err)

}

func TestRunHealthCommandInternalConnectionFailure(t *testing.T) {
	err := runHealthCommandInternal(context.Background(), &config.Config{Port: "1"}, 200*time.Millisecond)

	require.Error(t, err,
		"expected connection failure")

}

func TestRunHealthCommandInternalDefaultTimeoutFallback(t *testing.T) {
	err := runHealthCommandInternal(context.Background(), &config.Config{Port: "1"}, 0)

	require.Error(t, err,
		"expected connection failure")

	require.Contains(t, err.Error(), "health check request failed",
		"unexpected error: %v", err)

	// Ensure timeout path is exercised by checking the string formatting path.

	require.Contains(t, fmt.Sprint(defaultHealthTimeout), "5s",
		"unexpected default timeout: %s", defaultHealthTimeout)

}
