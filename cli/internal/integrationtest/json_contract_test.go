package integrationtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clipkg "github.com/getarcaneapp/arcane/cli/pkg"
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

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "arcanecli.yml")
	configContent := strings.Join([]string{
		"server_url: " + srv.URL,
		"api_key: arc_test_key",
		"default_environment: \"0\"",
		"log_level: info",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	root := clipkg.RootCommand()
	errOut := &strings.Builder{}
	root.SetErr(errOut)
	root.SetArgs([]string{"--config", configPath, "containers", "list", "--json"})

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		_ = w.Close()
		os.Stdout = stdout
	}()

	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v (%s)", err, errOut.String())
	}
	_ = w.Close()
	stdoutBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	outBuf := string(stdoutBytes)
	if runtime.GOOS == "windows" {
		outBuf = strings.ReplaceAll(outBuf, "\r\n", "\n")
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
