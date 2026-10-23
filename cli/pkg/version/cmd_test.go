package version_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/config"
	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

func TestVersionOutput(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		json        bool
		unavailable bool
	}{
		{name: "both versions", status: http.StatusOK, body: `{"currentVersion":"1.2.3","displayVersion":"v1.2.3","revision":"server-revision"}`},
		{name: "server JSON", status: http.StatusOK, body: `{"currentVersion":"1.2.3","displayVersion":"v1.2.3","revision":"server-revision"}`, json: true},
		{name: "unavailable text", status: http.StatusUnauthorized, body: `{"error":"unauthorized"}`, unavailable: true},
		{name: "unavailable JSON", status: http.StatusUnauthorized, body: `{"error":"unauthorized"}`, json: true, unavailable: true},
		{name: "invalid server response", status: http.StatusOK, body: `invalid JSON`, unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/app/version" {
					t.Errorf("unexpected version endpoint: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			args := []string{"--config", testutil.WriteConfig(t, srv.URL), "--no-color", "version"}
			if tc.json {
				args = append(args, "--json")
			}
			out, diagnostics, err := testutil.Execute(t, args)
			if tc.json && tc.unavailable {
				require.Error(t, err)
				require.Empty(t, out)
				return
			}
			require.NoError(t, err)
			if tc.json {
				var got map[string]any
				require.NoError(t, json.Unmarshal([]byte(out), &got))
				require.Equal(t, "v1.2.3", got["displayVersion"])
				require.Equal(t, "server-revision", got["revision"])
				require.NotContains(t, out, "Arcane CLI")
				return
			}
			require.Contains(t, out, "Arcane CLI")
			require.Contains(t, out, config.Version)
			require.Contains(t, out, config.Revision)
			require.Contains(t, out, "Arcane Server")
			if tc.unavailable {
				require.Contains(t, out, "Unavailable")
				require.Contains(t, diagnostics, "Could not get server version")
			} else {
				require.Contains(t, out, "v1.2.3")
				require.Contains(t, out, "server-revision")
			}
		})
	}
}
