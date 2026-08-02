package integrationtest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestContainerStartSurfacesServerError verifies that `containers start` reports
// a non-2xx response from the action endpoint as an error instead of printing a
// false success. This is a regression guard: the manager's remote-environment
// proxy returns 403 when the caller lacks the required per-environment
// permission, and the CLI must not swallow that and exit 0.

func TestContainerStartSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/0/containers/abc123":
			// Resolution succeeds so the action POST below is reached.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":"abc123","name":"nginx"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/0/containers/abc123/start":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"data":{"error":"permission denied: containers:start"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
		}
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "containers", "start", "abc123"},
	)

	require.Error(t, err,
		"expected an error for a 403 start response, got success\nstdout=%s\nstderr=%s", outBuf, errOut)

	require.Contains(t, err.Error(), "403",
		"expected the error to mention status 403, got: %v", err)

	require.NotContains(t, outBuf, "successfully",
		"expected no success message on a denied start, got stdout=%s", outBuf)

}

// TestSystemContainersStartAllSurfacesServerError verifies the same guard for a
// system command that does not resolve a resource first: it must report the
// server's error rather than claiming success.
func TestSystemContainersStartAllSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/environments/0/system/containers/start-all" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"data":{"error":"permission denied: containers:start"}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":"not found"}`))
	}))
	defer srv.Close()

	configPath := writeCLIIntegrationConfigInternal(t, srv.URL)
	outBuf, errOut, err := executeCLIIntegrationCommandInternal(
		t,
		[]string{"--config", configPath, "system", "containers-start-all"},
	)

	require.Error(t, err,
		"expected an error for a 403 response, got success\nstdout=%s\nstderr=%s", outBuf, errOut)

	require.Contains(t, err.Error(), "403",
		"expected the error to mention status 403, got: %v", err)

	require.NotContains(t, outBuf, "Started all containers",
		"expected no success message on a denied start-all, got stdout=%s", outBuf)

}
