package updates_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/cli/v2/internal/testutil"
)

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

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "images", "updates", "check", imageRef, "--json"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	require.Equal(t, imageRef, receivedImageRef,
		"expected imageRef %q, got %q", imageRef, receivedImageRef)

	require.False(t, !strings.Contains(receivedRawQuery, "%26") || !strings.Contains(receivedRawQuery, "%3D"),
		"expected reserved query characters to be encoded, got %q", receivedRawQuery)

	require.NotContains(t, receivedRawQuery, "&debug=",
		"imageRef escaped into a second query parameter: %q", receivedRawQuery)

	var result types.ImageUpdateResponse
	{
		unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &result)
		require.NoError(t, unmarshalErr,
			"single response JSON parse failed: %v\noutput=%s", unmarshalErr, outBuf)
	}

	require.False(t, !result.HasUpdate || result.CurrentVersion != "1.0.0" || result.LatestVersion != "1.1.0",
		"unexpected single image update response: %+v", result)
}

func TestImagesUpdatesCheckRequiresImageRef(t *testing.T) {
	outBuf, _, err := testutil.Execute(t, []string{"images", "updates", "check"})

	require.Error(t, err,
		"expected missing image reference to fail, got output %q", outBuf)

	require.Contains(t, err.Error(), "at least one image reference",
		"unexpected argument error: %v", err)
}

func TestImageUpdateCommandsDecodeExpectedResponseTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"0","name":"Local","enabled":true},{"id":"remote","name":"Remote","enabled":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/images":
			_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/remote/images":
			_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"image-prefix-123","repoTags":["nginx:latest"]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/remote/image-updates/check/image-prefix-123":
			_, _ = w.Write([]byte(`{"success":true,"data":{"hasUpdate":false,"currentVersion":"remote-version"}}`))
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

	configPath := testutil.WriteConfig(t, srv.URL)
	outBuf, errOut, err := testutil.Execute(
		t,
		[]string{"--config", configPath, "images", "updates", "check-all", "--json"},
	)

	require.NoError(t, err,
		"check-all: %v (%s)", err, errOut)

	var batch types.ImageUpdateBatchResponse
	{
		unmarshalErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &batch)
		require.NoError(t, unmarshalErr,
			"batch response JSON parse failed: %v\noutput=%s", unmarshalErr, outBuf)
	}

	require.False(t, batch["nginx:latest"] == nil || !batch["nginx:latest"].HasUpdate,
		"unexpected batch response: %+v", batch)

	outBuf, errOut, err = testutil.Execute(
		t,
		[]string{"--config", configPath, "images", "updates", "check-image", "image-123", "--json"},
	)

	require.NoError(t, err,
		"check-image: %v (%s)", err, errOut)

	var single types.ImageUpdateResponse
	{
		decodeSingleResponseErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &single)
		require.NoError(t, decodeSingleResponseErr,
			"single response JSON parse failed: %v\noutput=%s", decodeSingleResponseErr, outBuf)
	}

	require.False(t, single.HasUpdate || single.CurrentVersion != "1.1.0",
		"unexpected image response: %+v", single)

	outBuf, errOut, err = testutil.Execute(
		t,
		[]string{"--config", configPath, "images", "updates", "summary", "--json"},
	)

	require.NoError(t, err,
		"summary: %v (%s)", err, errOut)

	var summary types.ImageUpdateSummary
	{
		decodeSummaryResponseErr := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &summary)
		require.NoError(t, decodeSummaryResponseErr,
			"summary response JSON parse failed: %v\noutput=%s", decodeSummaryResponseErr, outBuf)
	}

	require.False(t, summary.TotalImages != 4 || summary.ImagesWithUpdates != 2 || summary.DigestUpdates != 1,
		"unexpected summary response: %+v", summary)

	previewPath := testutil.WriteConfig(t, srv.URL, true)
	outBuf, errOut, err = testutil.Execute(t, []string{"--config", previewPath, "images", "updates", "check-image", "image-prefix", "--json"})
	require.NoError(t, err, errOut)
	require.NoError(t, json.Unmarshal([]byte(outBuf), &single))
	require.Equal(t, "remote-version", single.CurrentVersion)
	require.Contains(t, errOut, "Remote (remote)")
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

			configPath := testutil.WriteConfig(t, srv.URL)
			for _, command := range commands {
				t.Run(command.name, func(t *testing.T) {
					args := append([]string{"--config", configPath}, command.args...)
					outBuf, _, err := testutil.Execute(t, args)

					require.Error(t, err,
						"expected %s error, got success with output %q", failure.name, outBuf)

					require.Contains(t, err.Error(), failure.wantError,
						"expected error containing %q, got %v", failure.wantError, err)

					require.Empty(t, strings.TrimSpace(outBuf),
						"failed response produced success output: %q", outBuf)
				})
			}
		})
	}
}
