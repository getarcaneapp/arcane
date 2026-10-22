package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getarcaneapp/arcane/backend/v3/internal/docker"
)

func newSwarmTestDockerClient(t *testing.T, server *httptest.Server) *client.Client {
	t.Helper()
	cli, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func TestService_ScaleService_HandlesServiceModes(t *testing.T) {
	ctx := t.Context()
	replicas := uint64(5)
	maxConcurrent := uint64(2)

	tests := []struct {
		name       string
		mode       swarm.ServiceMode
		assertMode func(*testing.T, swarm.ServiceMode)
		wantErr    bool
	}{
		{
			name: "replicated",
			mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{}},
			assertMode: func(t *testing.T, mode swarm.ServiceMode) {
				t.Helper()
				require.NotNil(t, mode.Replicated)
				require.NotNil(t, mode.Replicated.Replicas)
				require.Equal(t, replicas, *mode.Replicated.Replicas)
				require.Nil(t, mode.ReplicatedJob)
			},
		},
		{
			name: "replicated job",
			mode: swarm.ServiceMode{ReplicatedJob: &swarm.ReplicatedJob{MaxConcurrent: &maxConcurrent}},
			assertMode: func(t *testing.T, mode swarm.ServiceMode) {
				t.Helper()
				require.Nil(t, mode.Replicated)
				require.NotNil(t, mode.ReplicatedJob)
				require.NotNil(t, mode.ReplicatedJob.TotalCompletions)
				require.Equal(t, replicas, *mode.ReplicatedJob.TotalCompletions)
				require.NotNil(t, mode.ReplicatedJob.MaxConcurrent)
				require.Equal(t, maxConcurrent, *mode.ReplicatedJob.MaxConcurrent)
			},
		},
		{
			name:    "global",
			mode:    swarm.ServiceMode{Global: &swarm.GlobalService{}},
			wantErr: true,
		},
		{
			name:    "global job",
			mode:    swarm.ServiceMode{GlobalJob: &swarm.GlobalJob{}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateCalls := 0
			var updatedSpec swarm.ServiceSpec

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1.41/info":
					if !assert.NoError(t, json.NewEncoder(w).Encode(system.Info{
						Swarm: swarm.Info{
							LocalNodeState:   swarm.LocalNodeStateActive,
							ControlAvailable: true,
						},
					})) {
						return
					}
				case r.Method == http.MethodGet && r.URL.Path == "/v1.41/services/service-1":
					if !assert.NoError(t, json.NewEncoder(w).Encode(swarm.Service{
						ID:      "service-1",
						Version: swarm.Version{Index: 7},
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{Name: "service-1"},
							Mode:        tt.mode,
						},
					})) {
						return
					}
				case r.Method == http.MethodPost && r.URL.Path == "/v1.41/services/service-1/update":
					updateCalls++
					if !assert.Equal(t, "7", r.URL.Query().Get("version")) {
						return
					}
					if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&updatedSpec)) {
						return
					}
					if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Warnings": []string{"updated"}})) {
						return
					}
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			dockerService := (&docker.DockerClientService{}).WithClient(newSwarmTestDockerClient(t, server))
			svc := NewService(dockerService, func(context.Context) error { return nil }, nil)

			resp, err := svc.ScaleService(ctx, "service-1", replicas)
			if tt.wantErr {
				require.Error(t, err)
				require.True(t, errdefs.IsInvalidArgument(err), "expected invalid argument, got %v", err)
				require.Equal(t, 0, updateCalls)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, []string{"updated"}, resp.Warnings)
			require.Equal(t, 1, updateCalls)
			tt.assertMode(t, updatedSpec.Mode)
		})
	}
}
