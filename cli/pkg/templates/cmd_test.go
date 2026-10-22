package templates_test

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

func TestTemplatesListJSONIncludesPaginatedEnvelope(t *testing.T) {
	var (
		mu       sync.Mutex
		gotPath  string
		gotQuery string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{"name":"nginx","isCustom":false,"isRemote":false,"description":"Nginx template"}
			],
			"pagination": {"totalPages":3,"totalItems":15,"currentPage":3,"itemsPerPage":7}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "templates", "list", "--json", "--limit", "7", "--start", "14"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	if gotPath != "/api/templates" {
		mu.Unlock()
		require.FailNowf(t, "unexpected failure", "path = %q, want %q", gotPath, "/api/templates")
	}
	if !strings.Contains(gotQuery, "limit=7") || !strings.Contains(gotQuery, "start=14") {
		mu.Unlock()
		require.FailNowf(t, "unexpected failure", "query = %q, want limit=7 and start=14", gotQuery)
	}
	mu.Unlock()

	var got map[string]any
	{
		unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, unmarshalErr,
			"json parse failed: %v\noutput=%s", unmarshalErr, outBuf)
	}
	{

		_, ok := got["success"]
		require.True(t, ok,
			"expected success key in output: %v", got)
	}
	{

		_, ok := got["data"]
		require.True(t, ok,
			"expected data key in output: %v", got)
	}
	{

		_, ok := got["pagination"]
		require.True(t, ok,
			"expected pagination key in output: %v", got)
	}
}

func TestTemplatesListAllRejectsExplicitPaginationFlags(t *testing.T) {
	var (
		mu     sync.Mutex
		called bool
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		called = true
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "templates", "list", "--all", "--limit", "50"},
	)

	require.Error(t, err,
		"expected command error, got nil")

	require.Contains(t, err.Error(), "flags --all and --limit cannot be used together",
		"unexpected error: %v", err)

	require.Contains(t, errOut, "flags --all and --limit cannot be used together",
		"unexpected stderr output: %s", errOut)

	mu.Lock()
	defer mu.Unlock()

	require.False(t, called,
		"expected command to fail before issuing HTTP request")
}
