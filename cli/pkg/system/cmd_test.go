package system_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

func TestSystemContainersStartAllSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/environments/0/system/containers" && r.URL.Query().Get("action") == "startall" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"data":{"error":"permission denied: containers:start"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "--env", "0", "system", "containers", "start"},
	)

	require.Error(t, err,
		"expected an error for a 403 response, got success\nstdout=%s\nstderr=%s", outBuf, errOut)

	require.Contains(t, err.Error(), "403",
		"expected the error to mention status 403, got: %v", err)

	require.NotContains(t, outBuf, "Started all containers",
		"expected no success message on a denied start-all, got stdout=%s", outBuf)
	for _, tc := range []struct {
		name    string
		partial bool
		cancel  bool
	}{
		{name: "all environments succeed"}, {name: "continues after failure", partial: true}, {name: "requires consent without a terminal", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var mutations []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/environments" {
					_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"0","name":"Local","enabled":true},{"id":"remote","name":"Remote","enabled":true}]}`))
					return
				}
				mu.Lock()
				mutations = append(mutations, r.URL.RequestURI())
				mu.Unlock()
				if tc.partial && strings.Contains(r.URL.Path, "/0/") {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"success":false,"error":"start denied"}`))
					return
				}
				_, _ = w.Write([]byte(`{"success":true,"data":{"message":"started"}}`))
			}))
			defer server.Close()
			path := testutil.WriteConfig(t, server.URL)
			args := []string{"--config", path, "--json"}
			if !tc.cancel {
				args = append(args, "--yes")
			}
			out, diagnostics, runErr := testutil.Execute(t, append(args, "system", "containers", "start"))
			mu.Lock()
			paths := append([]string(nil), mutations...)
			mu.Unlock()
			if tc.cancel {
				require.ErrorContains(t, runErr, "re-run with --yes")
				require.Empty(t, paths)
				require.Empty(t, out)
				require.ErrorContains(t, runErr, "in all 2 environments?")
				require.NotContains(t, runErr.Error(), "Local")
				return
			}
			require.Equal(t, []string{"/api/environments/0/system/containers?action=startall", "/api/environments/remote/system/containers?action=startall"}, paths)
			if tc.partial {
				require.ErrorContains(t, runErr, "start denied")
			} else {
				require.NoError(t, runErr, diagnostics)
			}
			var result struct {
				Success      bool              `json:"success"`
				Environments []json.RawMessage `json:"environments"`
			}
			require.NoError(t, json.Unmarshal([]byte(out), &result), out)
			require.Equal(t, !tc.partial, result.Success)
			require.Len(t, result.Environments, 2)
		})
	}
}
