package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	if err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut)
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
