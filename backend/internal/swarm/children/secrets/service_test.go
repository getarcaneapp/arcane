package secrets

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

	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
)

func TestService_UpdateSecret_ReplacesLabelsOnly(t *testing.T) {
	current := swarm.Secret{
		ID:      "secret-1",
		Version: swarm.Version{Index: 4},
		Spec: swarm.SecretSpec{
			Annotations: swarm.Annotations{
				Name:   "db-pass",
				Labels: map[string]string{"old": "label"},
			},
			Driver: &swarm.Driver{Name: "vault"},
		},
	}
	var updatedSpec map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.41/secrets/secret-1":
			assert.NoError(t, json.NewEncoder(w).Encode(current))
		case r.Method == http.MethodPost && r.URL.Path == "/v1.41/secrets/secret-1/update":
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

	svc := NewService(&docker.DockerClientService{Client: cli}, func(context.Context) error {
		return nil
	})
	updated, err := svc.UpdateSecret(t.Context(), "secret-1", types.SwarmSecretUpdateRequest{
		Version: 4,
		Labels:  map[string]string{"tier": "db"},
	})
	require.NoError(t, err)

	assert.Equal(t, "db-pass", updatedSpec["Name"])
	assert.Equal(t, map[string]any{"tier": "db"}, updatedSpec["Labels"])
	assert.Equal(t, map[string]any{"Name": "vault"}, updatedSpec["Driver"])
	assert.NotContains(t, updatedSpec, "Data")
	assert.Equal(t, uint64(5), updated.Version.Index)
	assert.Equal(t, map[string]string{"tier": "db"}, updated.Spec.Labels)
}
