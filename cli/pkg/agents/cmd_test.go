package agents_test

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/cli/v3/internal/testutil"
)

func TestEnvironmentsCreatePrintsEnrollmentTokenAndInstallCommands(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents":
			assert.NoError(t, json.UnmarshalRead(r.Body, &createBody))
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"env-1","name":"Agent One","apiUrl":"agent://agent-one","status":"pending","enabled":true}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/agents/env-1/deployment":
			_, _ = w.Write([]byte(`{"success":true,"data":{"enrollmentToken":"arc_etkn_secret","dockerRun":"docker run arcane-agent","dockerCompose":"services:\n  arcane-agent: {}"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
		}
	}))
	defer srv.Close()

	out, errOut, err := testutil.Execute(t, []string{"--config", testutil.WriteConfig(t, srv.URL), "agents", "create", "--name", "Agent One"})
	require.NoError(t, err, "stdout=%s\nstderr=%s", out, errOut)

	require.Equal(t, map[string]any{"name": "Agent One", "enabled": true}, createBody)
	require.Contains(t, out, "arc_etkn_secret")
	require.Contains(t, out, "docker run arcane-agent")
	require.Contains(t, out, "arcane-agent: {}")
}

func TestEnvironmentsTunnelRestartPrintsSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/api/agents/env-1/tunnel/restart" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"reconnected":true,"sessionId":"session-new"}}`))
	}))
	defer srv.Close()

	out, errOut, err := testutil.Execute(t, []string{"--config", testutil.WriteConfig(t, srv.URL), "agents", "tunnel", "restart", "env-1"})
	require.NoError(t, err, "stdout=%s\nstderr=%s", out, errOut)
	require.Contains(t, out, "Agent tunnel reconnected")
	require.Contains(t, out, "session-new")
}
