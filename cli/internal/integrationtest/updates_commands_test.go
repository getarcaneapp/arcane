package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/imageupdate"
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

func TestImagesUpdatesCheckEncodesImageRefAndDecodesSingleResponse(t *testing.T) {
	imageRef := "registry.example.com/team/image:latest&debug=true"
	receivedImageRef := ""
	receivedRawQuery := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/environments/0/image-updates/check" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}

		receivedImageRef = r.URL.Query().Get("imageRef")
		receivedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"hasUpdate": true,
				"updateType": "minor",
				"currentVersion": "1.0.0",
				"latestVersion": "1.1.0",
				"checkTime": "2026-07-09T12:00:00Z",
				"responseTimeMs": 17
			}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "images", "updates", "check", imageRef, "--json"},
	)
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
	}
	if receivedImageRef != imageRef {
		t.Fatalf("expected imageRef %q, got %q", imageRef, receivedImageRef)
	}
	if !strings.Contains(receivedRawQuery, "%26") || !strings.Contains(receivedRawQuery, "%3D") {
		t.Fatalf("expected reserved query characters to be encoded, got %q", receivedRawQuery)
	}
	if strings.Contains(receivedRawQuery, "&debug=") {
		t.Fatalf("imageRef escaped into a second query parameter: %q", receivedRawQuery)
	}

	var result imageupdate.Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &result); err != nil {
		t.Fatalf("single response JSON parse failed: %v\noutput=%s", err, outBuf)
	}
	if !result.HasUpdate || result.CurrentVersion != "1.0.0" || result.LatestVersion != "1.1.0" {
		t.Fatalf("unexpected single image update response: %+v", result)
	}
}

func TestImagesUpdatesCheckRequiresImageRef(t *testing.T) {
	outBuf, _, err := executeCLIIntegrationCommandInternal(t, []string{"images", "updates", "check"})
	if err == nil {
		t.Fatalf("expected missing image reference to fail, got output %q", outBuf)
	}
	if !strings.Contains(err.Error(), "1 arg") || !strings.Contains(err.Error(), "received 0") {
		t.Fatalf("unexpected argument error: %v", err)
	}
}

func TestImageUpdateCommandsDecodeExpectedResponseTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/0/image-updates/check-all":
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"nginx:latest": {
						"hasUpdate": true,
						"updateType": "digest",
						"currentVersion": "latest",
						"latestVersion": "latest",
						"checkTime": "2026-07-09T12:00:00Z",
						"responseTimeMs": 21
					}
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/image-updates/check/image-123":
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"hasUpdate": false,
					"updateType": "none",
					"currentVersion": "1.1.0",
					"checkTime": "2026-07-09T12:00:00Z",
					"responseTimeMs": 9
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/image-updates/summary":
			_, _ = w.Write([]byte(`{
				"success": true,
				"data": {
					"totalImages": 4,
					"imagesWithUpdates": 2,
					"digestUpdates": 1,
					"errorsCount": 0
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
		}
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "images", "updates", "check-all", "--json"},
	)
	if err != nil {
		t.Fatalf("check-all: %v (%s)", err, errOut)
	}
	var batch imageupdate.BatchResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &batch); err != nil {
		t.Fatalf("batch response JSON parse failed: %v\noutput=%s", err, outBuf)
	}
	if batch["nginx:latest"] == nil || !batch["nginx:latest"].HasUpdate {
		t.Fatalf("unexpected batch response: %+v", batch)
	}

	outBuf, errOut, err = executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "images", "updates", "check-image", "image-123", "--json"},
	)
	if err != nil {
		t.Fatalf("check-image: %v (%s)", err, errOut)
	}
	var single imageupdate.Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &single); err != nil {
		t.Fatalf("single response JSON parse failed: %v\noutput=%s", err, outBuf)
	}
	if single.HasUpdate || single.CurrentVersion != "1.1.0" {
		t.Fatalf("unexpected image response: %+v", single)
	}

	outBuf, errOut, err = executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "images", "updates", "summary", "--json"},
	)
	if err != nil {
		t.Fatalf("summary: %v (%s)", err, errOut)
	}
	var summary imageupdate.Summary
	if err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &summary); err != nil {
		t.Fatalf("summary response JSON parse failed: %v\noutput=%s", err, outBuf)
	}
	if summary.TotalImages != 4 || summary.ImagesWithUpdates != 2 || summary.DigestUpdates != 1 {
		t.Fatalf("unexpected summary response: %+v", summary)
	}
}

func TestImageUpdateCommandsRejectHTTPAndEnvelopeFailures(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{name: "check", args: []string{"images", "updates", "check", "nginx:latest"}},
		{name: "check-all", args: []string{"images", "updates", "check-all"}},
		{name: "check-image", args: []string{"images", "updates", "check-image", "image-123"}},
		{name: "summary", args: []string{"images", "updates", "summary"}},
	}
	failures := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{
			name:       "non-2xx",
			statusCode: http.StatusBadGateway,
			body:       `{"success":true,"data":{}}`,
			wantError:  "status 502",
		},
		{
			name:       "success false",
			statusCode: http.StatusOK,
			body:       `{"success":false,"error":"permission denied"}`,
			wantError:  "API error: permission denied",
		},
	}

	for _, failure := range failures {
		t.Run(failure.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(failure.statusCode)
				_, _ = w.Write([]byte(failure.body))
			}))
			defer srv.Close()

			configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
			for _, command := range commands {
				t.Run(command.name, func(t *testing.T) {
					args := append([]string{"--config", configPath}, command.args...)
					outBuf, _, err := executeCLIIntegrationCommandInternal(t, args)
					if err == nil {
						t.Fatalf("expected %s error, got success with output %q", failure.name, outBuf)
					}
					if !strings.Contains(err.Error(), failure.wantError) {
						t.Fatalf("expected error containing %q, got %v", failure.wantError, err)
					}
					if strings.TrimSpace(outBuf) != "" {
						t.Fatalf("failed response produced success output: %q", outBuf)
					}
				})
			}
		})
	}
}
