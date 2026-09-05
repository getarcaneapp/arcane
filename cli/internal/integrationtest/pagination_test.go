package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainersListSendsLimitAndStart(t *testing.T) {
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
				{"id":"abc123","names":["/nginx"],"image":"nginx:latest","state":"running","status":"Up 1 hour"}
			],
			"pagination": {"totalPages":4,"totalItems":20,"currentPage":3,"itemsPerPage":5}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "list", "--json", "--limit", "5", "--start", "10"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/environments/0/containers", gotPath,
		"path = %q, want %q", gotPath, "/api/environments/0/containers")

	require.True(t, strings.Contains(gotQuery, "limit=5") && strings.Contains(gotQuery, "start=10"),
		"query = %q, want limit=5 and start=10", gotQuery)

	var got map[string]any
	{
		err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, err,
			"json parse failed: %v\noutput=%s", err, outBuf)
	}
	{

		_, ok := got["pagination"]
		require.True(t, ok,
			"expected pagination in output: %v", got)
	}

}

func TestContainersListExplicitStartZeroSendsStartZero(t *testing.T) {
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
			"data": [],
			"pagination": {"totalPages":2,"totalItems":26,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	_, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "list", "--json", "--start", "0"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/environments/0/containers", gotPath,
		"path = %q, want %q", gotPath, "/api/environments/0/containers")

	require.True(t, strings.Contains(gotQuery, "start=0") && strings.Contains(gotQuery, "limit=20"),
		"query = %q, want start=0 and limit=20", gotQuery)

}

func TestContainersListTextShowsShowingSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{"id":"abc123","names":["/nginx"],"image":"nginx:latest","state":"running","status":"Up 1 hour"}
			],
			"pagination": {"totalPages":2,"totalItems":26,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "list", "--json=false"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Contains(t, outBuf, "Showing: 1/26 containers",
		"expected showing summary in output, got:\n%s", outBuf)

}

// TestContainersListAllRequestsEveryItem pins the wire form of --all. The API
// has no "all" query parameter — it silently ignores unknown ones — so sending
// all=true without a limit left Huma applying its default of 20. limit=-1 is the
// documented "return everything" sentinel.
func TestContainersListAllRequestsEveryItem(t *testing.T) {
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
			"data": [],
			"pagination": {"totalPages":1,"totalItems":0,"currentPage":1,"itemsPerPage":0}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	_, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "list", "--json", "--all"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/environments/0/containers", gotPath,
		"path = %q, want %q", gotPath, "/api/environments/0/containers")

	require.Equal(t, "limit=-1", gotQuery,
		"query = %q, want %q", gotQuery, "limit=-1")

}

// TestProjectsListAllIncludesArchived covers the one resource where "everything"
// takes a second parameter: the projects list filters archived rows out by
// default, so --all has to opt back into them.
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

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	_, errOut, err := executeCLIIntegrationCommandInternal(
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

func TestAdminEventsListEnvSendsLimitAndStart(t *testing.T) {
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
				{
					"id":"evt-1",
					"type":"container.start",
					"severity":"info",
					"title":"Container started",
					"timestamp":"2026-04-08T12:00:00Z",
					"createdAt":"2026-04-08T12:00:00Z"
				}
			],
			"pagination": {"totalPages":5,"totalItems":15,"currentPage":3,"itemsPerPage":3}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	_, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "admin", "events", "list", "--environment", "--json", "--limit", "3", "--start", "6"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	mu.Lock()
	defer mu.Unlock()

	require.Equal(t, "/api/events/environment/0", gotPath,
		"path = %q, want %q", gotPath, "/api/events/environment/0")

	require.True(t, strings.Contains(gotQuery, "limit=3") && strings.Contains(gotQuery, "start=6"),
		"query = %q, want limit=3 and start=6", gotQuery)

}

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

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
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
	if !(strings.Contains(gotQuery, "limit=7") && strings.Contains(gotQuery, "start=14")) {
		mu.Unlock()
		require.FailNowf(t, "unexpected failure", "query = %q, want limit=7 and start=14", gotQuery)
	}
	mu.Unlock()

	var got map[string]any
	{
		err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, err,
			"json parse failed: %v\noutput=%s", err, outBuf)
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

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	_, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "templates", "list", "--all", "--limit", "50"},
	)

	require.Error(t, err,
		"expected command error, got nil")

	require.Contains(t, err.Error(), "--all cannot be combined with explicit pagination flags",
		"unexpected error: %v", err)

	require.Contains(t, errOut, "--all cannot be combined with explicit pagination flags",
		"unexpected stderr output: %s", errOut)

	mu.Lock()
	defer mu.Unlock()

	require.False(t, called,
		"expected command to fail before issuing HTTP request")

}
