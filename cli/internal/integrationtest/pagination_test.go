package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}

	mu.Lock()
	defer mu.Unlock()

	if gotPath != "/api/environments/0/containers" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/environments/0/containers")
	}
	if !(strings.Contains(gotQuery, "limit=5") && strings.Contains(gotQuery, "start=10")) {
		t.Fatalf("query = %q, want limit=5 and start=10", gotQuery)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got); err != nil {
		t.Fatalf("json parse failed: %v\noutput=%s", err, outBuf)
	}
	if _, ok := got["pagination"]; !ok {
		t.Fatalf("expected pagination in output: %v", got)
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
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}

	mu.Lock()
	defer mu.Unlock()

	if gotPath != "/api/environments/0/containers" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/environments/0/containers")
	}
	if !(strings.Contains(gotQuery, "start=0") && strings.Contains(gotQuery, "limit=20")) {
		t.Fatalf("query = %q, want start=0 and limit=20", gotQuery)
	}
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
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}
	if !strings.Contains(outBuf, "Showing: 1/26 containers") {
		t.Fatalf("expected showing summary in output, got:\n%s", outBuf)
	}
}

func TestContainersListAllSkipsPaginationParams(t *testing.T) {
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
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}

	mu.Lock()
	defer mu.Unlock()

	if gotPath != "/api/environments/0/containers" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/environments/0/containers")
	}
	if gotQuery != "all=true" {
		t.Fatalf("query = %q, want %q", gotQuery, "all=true")
	}
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
		[]string{"--config", configPath, "admin", "events", "list-env", "--json", "--limit", "3", "--start", "6"},
	)
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}

	mu.Lock()
	defer mu.Unlock()

	if gotPath != "/api/events/environment/0" {
		t.Fatalf("path = %q, want %q", gotPath, "/api/events/environment/0")
	}
	if !(strings.Contains(gotQuery, "limit=3") && strings.Contains(gotQuery, "start=6")) {
		t.Fatalf("query = %q, want limit=3 and start=6", gotQuery)
	}
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
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}

	mu.Lock()
	if gotPath != "/api/templates" {
		mu.Unlock()
		t.Fatalf("path = %q, want %q", gotPath, "/api/templates")
	}
	if !(strings.Contains(gotQuery, "limit=7") && strings.Contains(gotQuery, "start=14")) {
		mu.Unlock()
		t.Fatalf("query = %q, want limit=7 and start=14", gotQuery)
	}
	mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got); err != nil {
		t.Fatalf("json parse failed: %v\noutput=%s", err, outBuf)
	}
	if _, ok := got["success"]; !ok {
		t.Fatalf("expected success key in output: %v", got)
	}
	if _, ok := got["data"]; !ok {
		t.Fatalf("expected data key in output: %v", got)
	}
	if _, ok := got["pagination"]; !ok {
		t.Fatalf("expected pagination key in output: %v", got)
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
	if err == nil {
		t.Fatal("expected command error, got nil")
	}
	if !strings.Contains(err.Error(), "--all cannot be combined with explicit pagination flags") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errOut, "--all cannot be combined with explicit pagination flags") {
		t.Fatalf("unexpected stderr output: %s", errOut)
	}
	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("expected command to fail before issuing HTTP request")
	}
}
