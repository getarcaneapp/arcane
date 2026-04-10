package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectsJSONCompatibilityForComposeUnitBytes(t *testing.T) {
	projectResponse := `{
		"success": true,
		"data": {
			"id": "project-1",
			"name": "myproject",
			"path": "/tmp/myproject",
			"status": "running",
			"serviceCount": 1,
			"runningCount": 1,
			"createdAt": "2026-03-13T00:00:00Z",
			"updatedAt": "2026-03-13T00:00:00Z",
			"services": [
				{
					"name": "myapp",
					"image": "nginx:latest",
					"mem_limit": "256m",
					"build": {
						"context": ".",
						"shm_size": "64m"
					},
					"deploy": {
						"resources": {
							"limits": {
								"memory": "512m"
							}
						}
					}
				}
			],
			"runtimeServices": [
				{
					"name": "myapp",
					"image": "nginx:latest",
					"status": "running",
					"serviceConfig": {
						"name": "myapp",
						"image": "nginx:latest",
						"mem_limit": "256m"
					}
				}
			]
		}
	}`

	downCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/projects/myproject":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(projectResponse))
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/0/projects/project-1/down":
			downCalled = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"message":"stopped"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
		}
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)

	getStdout, getStderr, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "projects", "get", "myproject", "--json"},
	)
	if err != nil {
		t.Fatalf("projects get failed: %v (%s)", err, getStderr)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(getStdout)), &decoded); err != nil {
		t.Fatalf("projects get produced invalid JSON: %v\noutput=%s", err, getStdout)
	}

	services, ok := decoded["services"].([]any)
	if !ok || len(services) != 1 {
		t.Fatalf("expected one service in JSON output, got %#v", decoded["services"])
	}

	serviceMap, ok := services[0].(map[string]any)
	if !ok {
		t.Fatalf("expected service entry to be a map, got %#v", services[0])
	}

	if got := serviceMap["mem_limit"]; got != "268435456" {
		t.Fatalf("expected mem_limit to round-trip as normalized bytes, got %#v", got)
	}

	_, downStderr, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "projects", "down", "myproject"},
	)
	if err != nil {
		t.Fatalf("projects down failed: %v (%s)", err, downStderr)
	}
	if !downCalled {
		t.Fatal("expected projects down to issue a POST after resolving the project")
	}
}
