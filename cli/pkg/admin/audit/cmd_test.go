package audit_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v3/internal/testutil"
)

func TestAdminAuditExportStreamsToStdout(t *testing.T) {
	const export = "{\"id\":\"a1\",\"action\":\"role.create\"}\n{\"id\":\"a2\",\"action\":\"role.create\"}\n"
	requests := make(chan *url.URL, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(export))
	}))
	defer srv.Close()

	stdout, errOut, err := testutil.Execute(t, []string{
		"--config", testutil.WriteConfig(t, srv.URL), "admin", "audit", "export",
		"--format", "ndjson", "--action", "role.create", "--outcome", "success", "--since", "2026-10-01T00:00:00Z",
	})
	require.NoError(t, err, errOut)
	require.Equal(t, strings.Split(export, "\n"), strings.Split(stdout, "\n"))

	got := <-requests
	require.Equal(t, "/api/audit/logs/export", got.Path)
	require.Equal(t, url.Values{
		"format":  {"ndjson"},
		"action":  {"role.create"},
		"outcome": {"success"},
		"from":    {"2026-10-01T00:00:00Z"},
	}, got.Query())
}

func TestAdminAuditListRejectsInvalidSince(t *testing.T) {
	_, _, err := testutil.Execute(t, []string{
		"--config", testutil.WriteConfig(t, "http://127.0.0.1:1"), "admin", "audit", "list", "--since", "yesterday",
	})
	require.ErrorContains(t, err, `invalid --since "yesterday"`)
}
