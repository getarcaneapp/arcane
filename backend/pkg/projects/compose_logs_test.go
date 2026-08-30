package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	composeLogsTestProjectInternal         = "projlogdemo"
	composeLogsTestNonTTYContainerInternal = "11112222333344445555666677778888"
	composeLogsTestTTYContainerInternal    = "9999aaaabbbbccccddddeeeeffff0000"
	composeLogsTestTimestampInternal       = "2024-08-26T10:00:00.000000000Z "
)

func TestComposeLogsMarksStderrWithoutChangingComposeOptions(t *testing.T) {
	logsQueries := make(chan string, 4)

	summary := func(id, service, image string) map[string]any {
		return map[string]any{
			"Id":     id,
			"Names":  []string{"/" + composeLogsTestProjectInternal + "-" + service + "-1"},
			"Image":  image,
			"State":  "running",
			"Status": "Up",
			"Labels": map[string]string{
				"com.docker.compose.project":          composeLogsTestProjectInternal,
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
			require.NoError(t, json.NewEncoder(w).Encode([]map[string]any{
				summary(composeLogsTestNonTTYContainerInternal, "web", "nginx"),
				summary(composeLogsTestTTYContainerInternal, "ttyd", "ttyd"),
			}))
		case "/containers/" + composeLogsTestNonTTYContainerInternal + "/json":
			writeComposeInspectJSONInternal(t, w, composeLogsTestNonTTYContainerInternal, "web", false)
		case "/containers/" + composeLogsTestTTYContainerInternal + "/json":
			writeComposeInspectJSONInternal(t, w, composeLogsTestTTYContainerInternal, "ttyd", true)
		case "/containers/" + composeLogsTestNonTTYContainerInternal + "/logs":
			logsQueries <- r.URL.RawQuery
			w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
			_, err := w.Write(bytes.Join([][]byte{
				frameLogEntryInternal(stdcopy.Stdout, []byte(composeLogsTestTimestampInternal+"hello stdout\n")),
				frameLogEntryInternal(stdcopy.Stderr, []byte(composeLogsTestTimestampInternal+"hello stderr\n")),
			}, nil))
			require.NoError(t, err)
		case "/containers/" + composeLogsTestTTYContainerInternal + "/logs":
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			_, err := w.Write([]byte(composeLogsTestTimestampInternal + "raw tty output\n"))
			require.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(server.URL, "http://"))

	out := &bytes.Buffer{}
	err := ComposeLogs(context.Background(), composeLogsTestProjectInternal, out, false, "all", "", true)
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

func writeComposeInspectJSONInternal(t *testing.T, w http.ResponseWriter, containerID, serviceName string, tty bool) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
		"Id":      containerID,
		"Name":    "/" + composeLogsTestProjectInternal + "-" + serviceName + "-1",
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
