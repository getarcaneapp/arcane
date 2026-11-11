package settings_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"getarcane.dev/cli/v3/internal/testutil"
)

func TestSettingsSetSendsSingleKeyPatch(t *testing.T) {
	var (
		mu        sync.Mutex
		gotPath   string
		gotUpdate map[string]string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPatch {
			gotPath = r.URL.Path
			_ = json.UnmarshalRead(r.Body, &gotUpdate)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"settings":[],"runtime":{},"readOnly":false}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(t, []string{"--config", configPath, "--agent", "0", "settings", "set", "dockerApiTimeout", "60"})
	require.NoError(t, err, "execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/api/agents/0/settings", gotPath)
	require.Equal(t, map[string]string{"dockerApiTimeout": "60"}, gotUpdate)
}

func TestSettingsSetRoutesScopeToEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		wantPath string
		args     []string
	}{
		{name: "agent", args: []string{"--agent", "3", "settings", "set", "dockerApiTimeout", "60"}, wantPath: "/api/agents/3/settings"},
		{name: "manager", args: []string{"--agent", "3", "settings", "set", "--scope", "manager", "analyticsEnabled", "false"}, wantPath: "/api/settings"},
		{name: "authentication", args: []string{"--agent", "0", "settings", "set", "--scope", "authentication", "authSessionTimeout", "60"}, wantPath: "/api/settings/authentication"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu      sync.Mutex
				gotPath string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				if r.Method == http.MethodPatch {
					gotPath = r.URL.Path
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"settings":[],"runtime":{},"readOnly":false}`))
			}))
			defer srv.Close()

			configPath := testutil.WriteConfig(t, srv.URL)
			_, errOut, err := testutil.Execute(t, append([]string{"--config", configPath}, tt.args...))
			require.NoError(t, err, "execute: %v (%s)", err, errOut)

			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, tt.wantPath, gotPath)
		})
	}
}

func TestSettingsListRejectsUnknownScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"settings":[],"runtime":{},"readOnly":false}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, _, err := testutil.Execute(t, []string{"--config", configPath, "--agent", "0", "settings", "list", "--scope", "global"})
	require.EqualError(t, err, `invalid scope "global": must be agent, manager, or authentication`)
}

func TestSettingsSetReturnsFieldErrorMessage(t *testing.T) {
	const message = "dockerApiTimeout is set by environment variable ARCANE_DOCKER_API_TIMEOUT and cannot be changed"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		if r.Method != http.MethodPatch {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":"Bad Request","status":400,"detail":"` + message + `","details":{"field":"dockerApiTimeout"}}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, _, err := testutil.Execute(t, []string{"--config", configPath, "--agent", "0", "settings", "set", "dockerApiTimeout", "60"})
	require.EqualError(t, err, message)
}
