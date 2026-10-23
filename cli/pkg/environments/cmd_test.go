package environments_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

func TestEnvironmentsCreatePrintsAPIKeyAndInstallCommands(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments":
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"env-1","name":"Edge One","apiUrl":"edge://edge-one","status":"pending","enabled":true,"apiKey":"arc_secret"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/env-1/deployment":
			_, _ = w.Write([]byte(`{"success":true,"data":{"dockerRun":"docker run arcane-agent","dockerCompose":"services:\n  arcane-agent: {}"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
		}
	}))
	defer srv.Close()

	out, errOut, err := testutil.Execute(t, []string{"--config", testutil.WriteConfig(t, srv.URL), "environments", "create", "--name", "Edge One"})
	require.NoError(t, err, "stdout=%s\nstderr=%s", out, errOut)

	require.Equal(t, map[string]any{"name": "Edge One", "enabled": true}, createBody)
	require.Contains(t, out, "arc_secret")
	require.Contains(t, out, "docker run arcane-agent")
	require.Contains(t, out, "arcane-agent: {}")
}
