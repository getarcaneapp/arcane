package configs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
)

func TestService_UpdateConfig_KeepsDataAndReplacesLabels(t *testing.T) {
	current := swarm.Config{
		ID:      "config-1",
		Version: swarm.Version{Index: 4},
		Spec: swarm.ConfigSpec{
			Annotations: swarm.Annotations{
				Name:   "nginx-conf",
				Labels: map[string]string{"old": "label"},
			},
			Data: []byte("listen 80;"),
		},
	}
	var updatedSpec map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/configs/config-1":
			assert.NoError(t, json.NewEncoder(w).Encode(current))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/configs/config-1/update":
			assert.Equal(t, "4", r.URL.Query().Get("version"))
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&updatedSpec))
			current.Version.Index = 5
			current.Spec.Labels = map[string]string{"tier": "db"}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	svc := NewService((&docker.DockerClientService{}).WithClient(cli), func(context.Context) error {
		return nil
	})
	updated, err := svc.UpdateConfig(t.Context(), "config-1", types.SwarmConfigUpdateRequest{
		Version: 4,
		Labels:  map[string]string{"tier": "db"},
	})
	require.NoError(t, err)

	assert.Equal(t, "nginx-conf", updatedSpec["Name"])
	assert.Equal(t, map[string]any{"tier": "db"}, updatedSpec["Labels"])
	assert.Equal(t, "bGlzdGVuIDgwOw==", updatedSpec["Data"])
	assert.Equal(t, uint64(5), updated.Version.Index)
	assert.Equal(t, map[string]string{"tier": "db"}, updated.Spec.Labels)
}
