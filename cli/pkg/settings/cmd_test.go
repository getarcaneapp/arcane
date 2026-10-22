package settings_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
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
			_ = json.NewDecoder(r.Body).Decode(&gotUpdate)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"settings":[],"runtime":{},"readOnly":false}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(t, []string{"--config", configPath, "settings", "set", "dockerApiTimeout", "60"})
	require.NoError(t, err, "execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "/api/environments/0/settings", gotPath)
	require.Equal(t, map[string]string{"dockerApiTimeout": "60"}, gotUpdate)
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
	_, _, err := testutil.Execute(t, []string{"--config", configPath, "settings", "set", "dockerApiTimeout", "60"})
	require.EqualError(t, err, message)
}
