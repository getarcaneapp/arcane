package projects

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	composeLogsTestProject         = "projlogdemo"
	composeLogsTestNonTTYContainer = "11112222333344445555666677778888"
	composeLogsTestTTYContainer    = "9999aaaabbbbccccddddeeeeffff0000"
	composeLogsTestTimestamp       = "2024-08-26T10:00:00.000000000Z "
)

func TestComposeLogsMarksStderrWithoutChangingComposeOptions(t *testing.T) {
	logsQueries := make(chan string, 4)

	summary := func(id, service, image string) map[string]any {
		return map[string]any{
			"Id":     id,
			"Names":  []string{"/" + composeLogsTestProject + "-" + service + "-1"},
			"Image":  image,
			"State":  "running",
			"Status": "Up",
			"Labels": map[string]string{
				"com.docker.compose.project":          composeLogsTestProject,
				"com.docker.compose.service":          service,
				"com.docker.compose.container-number": "1",
				"com.docker.compose.oneoff":           "False",
			},
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := regexp.MustCompile(`^/v[0-9.]+`).ReplaceAllString(r.URL.Path, "")

		switch path {
		case "/_ping":
			w.Header().Set("Api-Version", "1.47")
			w.WriteHeader(http.StatusOK)
		case "/containers/json":
			w.Header().Set("Content-Type", "application/json")
			if !assert.NoError(t, json.NewEncoder(w).Encode([]map[string]any{
				summary(composeLogsTestNonTTYContainer, "web", "nginx"),
				summary(composeLogsTestTTYContainer, "ttyd", "ttyd"),
			})) {
				return
			}
		case "/containers/" + composeLogsTestNonTTYContainer + "/json":
			writeComposeInspectJSON(t, w, composeLogsTestNonTTYContainer, "web", false)
		case "/containers/" + composeLogsTestTTYContainer + "/json":
			writeComposeInspectJSON(t, w, composeLogsTestTTYContainer, "ttyd", true)
		case "/containers/" + composeLogsTestNonTTYContainer + "/logs":
			logsQueries <- r.URL.RawQuery
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
			_, err := w.Write(bytes.Join([][]byte{
				frameLogEntry(stdcopy.Stdout, []byte(composeLogsTestTimestamp+"hello stdout\n")),
				frameLogEntry(stdcopy.Stderr, []byte(composeLogsTestTimestamp+"hello stderr\n")),
			}, nil))
			if !assert.NoError(t, err) {
				return
			}
		case "/containers/" + composeLogsTestTTYContainer + "/logs":
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			_, err := w.Write([]byte(composeLogsTestTimestamp + "raw tty output\n"))
			if !assert.NoError(t, err) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:1")

	out := &bytes.Buffer{}
	err := ComposeLogs(t.Context(), "tcp://"+strings.TrimPrefix(server.URL, "http://"), composeLogsTestProject, out, types.LogStreamOptions{Tail: "all", Timestamps: true})
	require.NoError(t, err)

	body := out.String()
	require.Contains(t, body, "hello stdout")
	require.Contains(t, body, "| [STDERR] 2024-08-26T10:00:00.000000000Z hello stderr\n")
	require.Contains(t, body, "raw tty output")
	assert.NotContains(t, body, "[STDERR] raw tty")
	assert.NotContains(t, body, "[STDERR] hello stdout")

	query := <-logsQueries
	assert.Contains(t, query, "stdout=1")
	assert.Contains(t, query, "stderr=1")
	assert.Contains(t, query, "timestamps=1")
	assert.NotContains(t, query, "follow=")
	assert.NotContains(t, query, "tail=")
}

func writeComposeInspectJSON(t *testing.T, w http.ResponseWriter, containerID, serviceName string, tty bool) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"Id":      containerID,
		"Name":    "/" + composeLogsTestProject + "-" + serviceName + "-1",
		"Created": "2024-08-26T09:00:00Z",
		"Path":    "/bin/sh",
		"State": map[string]any{
			"Running":   true,
			"StartedAt": "2024-08-26T09:00:01Z",
		},
		"Config": map[string]any{
			"Tty":    tty,
			"Image":  "example-image",
			"Labels": map[string]string{},
		},
		"HostConfig":      map[string]any{},
		"NetworkSettings": map[string]any{},
		"Mounts":          []any{},
	}))
}
