package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContainersListUpdatesJSONContract(t *testing.T) {
	seenUpdatesFilter := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/environments/0/containers" {
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
					"id":"abc123",
					"names":["/nginx"],
					"image":"nginx:latest",
					"state":"running",
					"status":"Up 1 hour",
					"updateInfo":{"hasUpdate":true,"latestVersion":"1.28.0"}
				}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "list", "--updates", "has_update", "--json"},
	)
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}
	if seenUpdatesFilter != "has_update" {
		t.Fatalf("expected updates filter query param, got %q", seenUpdatesFilter)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got); err != nil {
		t.Fatalf("json parse failed: %v\noutput=%s", err, outBuf)
	}
	for _, key := range []string{"success", "data", "pagination"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in output: %v", key, got)
		}
	}
}

func TestContainersUpdatesCommandUsesHasUpdateFilter(t *testing.T) {
	seenUpdatesFilter := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/environments/0/containers" {
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
					"id":"abc123",
					"names":["/nginx"],
					"image":"nginx:latest",
					"state":"running",
					"status":"Up 1 hour",
					"updateInfo":{"hasUpdate":true,"latestVersion":"1.28.0"}
				}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "--output", "text", "containers", "updates"},
	)
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}
	if seenUpdatesFilter != "has_update" {
		t.Fatalf("expected updates filter query param, got %q", seenUpdatesFilter)
	}
	if strings.TrimSpace(outBuf) == "" {
		t.Fatal("expected output from containers updates command")
	}
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

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "projects", "list", "--updates", "has_update", "--json"},
	)
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}
	if seenUpdatesFilter != "has_update" {
		t.Fatalf("expected updates filter query param, got %q", seenUpdatesFilter)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got); err != nil {
		t.Fatalf("json parse failed: %v\noutput=%s", err, outBuf)
	}
	for _, key := range []string{"success", "data", "pagination"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in output: %v", key, got)
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

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "--output", "text", "projects", "updates"},
	)
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}
	if seenUpdatesFilter != "has_update" {
		t.Fatalf("expected updates filter query param, got %q", seenUpdatesFilter)
	}
	if strings.TrimSpace(outBuf) == "" {
		t.Fatal("expected output from projects updates command")
	}
}
