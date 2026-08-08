package rustic

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	rusticruntime "github.com/getarcaneapp/arcane/backend/v2/pkg/libarcane/rustic"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestRusticServiceRunUsesOfficialImage(t *testing.T) {
	t.Parallel()

	type createRequest struct {
		Image      string   `json:"Image"`
		Entrypoint []string `json:"Entrypoint"`
		Cmd        []string `json:"Cmd"`
		Env        []string `json:"Env"`
	}
	var created createRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/images/"):
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create"):
			require.NoError(t, json.NewDecoder(r.Body).Decode(&created))
			_, _ = w.Write([]byte(`{"Id":"rustic-test"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/rustic-test/start"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/rustic-test/wait"):
			_, _ = w.Write([]byte(`{"StatusCode":0}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/rustic-test/logs"):
			w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
			payload := []byte("snapshot-json\n")
			header := make([]byte, 8)
			header[0] = 1
			binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
			_, _ = w.Write(append(header, payload...))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/containers/rustic-test"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	output, err := NewRusticService(nil).Run(
		context.Background(),
		dockerClient,
		[]string{"RUSTIC_REPOSITORY=/repository"},
		nil,
		"secret",
		[]string{"snapshots", "--json"},
	)
	require.NoError(t, err)
	require.Equal(t, "snapshot-json", output)
	require.Equal(t, rusticruntime.DefaultImage, created.Image)
	require.Empty(t, created.Entrypoint)
	require.Equal(t, []string{"snapshots", "--json"}, created.Cmd)
	require.True(t, slices.Contains(created.Env, "RUSTIC_PASSWORD=secret"))
}
