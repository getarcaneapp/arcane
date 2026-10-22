package projects_test

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

func TestProjectsListAllIncludesArchived(t *testing.T) {
	var (
		mu       sync.Mutex
		gotQuery string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.RawQuery
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [],
			"pagination": {"totalPages":1,"totalItems":0,"currentPage":1,"itemsPerPage":0}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	_, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "projects", "list", "--json", "--all"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "archived=all&limit=-1", gotQuery,
		"query = %q, want %q", gotQuery, "archived=all&limit=-1")
}

func TestProjectsListUpdatesJSONContract(t *testing.T) {
	seenUpdatesFilter := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/environments/0/projects" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}

		seenUpdatesFilter = r.URL.Query().Get("updates")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"id":"project-1",
					"name":"demo",
					"path":"/tmp/demo",
					"status":"running",
					"serviceCount":1,
					"runningCount":1,
					"createdAt":"2026-04-02T00:00:00Z",
					"updatedAt":"2026-04-02T00:00:00Z",
					"updateInfo":{"status":"has_update","hasUpdate":true,"imageCount":1,"checkedImageCount":1,"imagesWithUpdates":1,"errorCount":0}
				}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "projects", "list", "--updates", "has_update", "--json"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Equal(t, "has_update", seenUpdatesFilter,
		"expected updates filter query param, got %q", seenUpdatesFilter)

	var got map[string]any
	{
		unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, unmarshalErr,
			"json parse failed: %v\noutput=%s", unmarshalErr, outBuf)
	}

	for _, key := range []string{"success", "data", "pagination"} {
		{
			_, ok := got[key]
			require.True(t, ok,
				"missing key %q in output: %v", key, got)
		}
	}
}

func TestProjectsUpdatesCommandUsesHasUpdateFilter(t *testing.T) {
	seenUpdatesFilter := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/environments/0/projects" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}

		seenUpdatesFilter = r.URL.Query().Get("updates")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"id":"project-1",
					"name":"demo",
					"path":"/tmp/demo",
					"status":"running",
					"serviceCount":1,
					"runningCount":1,
					"createdAt":"2026-04-02T00:00:00Z",
					"updatedAt":"2026-04-02T00:00:00Z",
					"updateInfo":{"status":"has_update","hasUpdate":true,"imageCount":1,"checkedImageCount":1,"imagesWithUpdates":1,"errorCount":0}
				}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "--output", "text", "projects", "updates"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Equal(t, "has_update", seenUpdatesFilter,
		"expected updates filter query param, got %q", seenUpdatesFilter)

	require.NotEmpty(t, strings.TrimSpace(outBuf),
		"expected output from projects updates command")
}
