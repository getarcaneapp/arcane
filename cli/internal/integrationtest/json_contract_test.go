package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainersListJSONContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/environments/0/containers") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{"id":"abc123","names":["/nginx"],"image":"nginx:latest","state":"running","status":"Up 1 hour"}
			],
			"pagination": {"totalPages":1,"totalItems":1,"currentPage":1,"itemsPerPage":20}
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)

	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "list", "--json"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	var got map[string]any
	{
		err := json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
		require.NoError(t, err,
			"json parse failed: %v\noutput=%s", err, outBuf)
	}

	for _, key := range []string{"success", "data", "pagination"} {
		{
			_, ok := got[key]
			require.True(t, ok,
				"missing key %q in output: %v", key, got)
		}

	}
}

func TestVariablesListJSONContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/variables" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": [
				{
					"id": "variable-1",
					"key": "EXAMPLE",
					"value": "value",
					"isSecret": false,
					"allEnvironments": true,
					"environmentIds": [],
					"createdAt": "2026-08-25T00:00:00Z"
				}
			]
		}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "variables", "list", "--json"},
	)

	require.NoError(t, err,
		"execute: %v (%s)", err, errOut)

	var got []map[string]any
	err = json.Unmarshal([]byte(strings.TrimSpace(outBuf)), &got)
	require.NoError(t, err,
		"json parse failed: %v\noutput=%s", err, outBuf)
	require.Len(t, got, 1)
	require.Equal(t, "variable-1", got[0]["id"])
}
