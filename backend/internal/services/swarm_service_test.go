package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/stretchr/testify/require"
)

func TestDecodeSwarmSpecInternal_AllowsEmptyObject(t *testing.T) {
	spec, err := decodeSwarmSpecInternal(json.RawMessage(`{}`))
	require.NoError(t, err)
	require.NotNil(t, spec.Labels)
	require.Empty(t, spec.Labels)
}

func TestDecodeSwarmSpecInternal_RejectsNull(t *testing.T) {
	_, err := decodeSwarmSpecInternal(json.RawMessage(`null`))
	require.EqualError(t, err, "swarm spec is required")
}

func TestDefaultSwarmListenAddrInternal(t *testing.T) {
	require.Equal(t, defaultSwarmListenAddr, defaultSwarmListenAddrInternal(""))
	require.Equal(t, defaultSwarmListenAddr, defaultSwarmListenAddrInternal("   "))
	require.Equal(t, "eth0:2377", defaultSwarmListenAddrInternal(" eth0:2377 "))
}

func TestSwarmService_FetchSwarmNodeIdentityViaEdgeInternal_UsesEnvironmentAccessToken(t *testing.T) {
	ctx := context.Background()
	db := setupEnvironmentServiceTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)
	envSvc := NewEnvironmentService(db, nil, nil, nil, settingsSvc, nil)

	accessToken := "token-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/swarm/node-identity", r.URL.Path)
		require.Equal(t, accessToken, r.Header.Get("X-API-Key"))
		require.Equal(t, accessToken, r.Header.Get("X-Arcane-Agent-Token"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"swarmNodeId":"node-1","hostname":"worker-1","role":"worker","engineVersion":"29.3.1","swarmActive":true}}`))
	}))
	defer server.Close()

	createTestEnvironmentWithState(
		t,
		db,
		"env-1",
		server.URL,
		string(models.EnvironmentStatusOnline),
		false,
		&accessToken,
	)

	svc := NewSwarmService(nil, nil, nil, nil, envSvc)

	identity, err := svc.fetchSwarmNodeIdentityViaEdgeInternal(ctx, "env-1")
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, "node-1", identity.SwarmNodeID)
	require.Equal(t, "worker-1", identity.Hostname)
	require.Equal(t, "worker", identity.Role)
	require.Equal(t, "29.3.1", identity.EngineVersion)
	require.True(t, identity.SwarmActive)
}

func TestSwarmService_UpdateAndGetStackSource_UsesStoredFilesWithoutSwarmManager(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	svc := NewSwarmService(nil, settingsSvc, nil, nil, nil)

	updated, err := svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		EnvContent:     "FOO=bar\n",
	})
	require.NoError(t, err)
	require.Equal(t, "demo-stack", updated.Name)

	composePath := filepath.Join(rootDir, "0", "demo-stack", "compose.yaml")
	envPath := filepath.Join(rootDir, "0", "demo-stack", ".env")
	require.FileExists(t, composePath)
	require.FileExists(t, envPath)

	source, err := svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, updated.ComposeContent, source.ComposeContent)
	require.Equal(t, updated.EnvContent, source.EnvContent)
}

func TestSwarmService_ScaleService_HandlesServiceModesInternal(t *testing.T) {
	ctx := context.Background()
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
					require.NoError(t, json.NewEncoder(w).Encode(system.Info{
						Swarm: swarm.Info{
							LocalNodeState:   swarm.LocalNodeStateActive,
							ControlAvailable: true,
						},
					}))
				case r.Method == http.MethodGet && r.URL.Path == "/v1.41/services/service-1":
					require.NoError(t, json.NewEncoder(w).Encode(swarm.Service{
						ID: "service-1",
						Meta: swarm.Meta{
							Version: swarm.Version{Index: 7},
						},
						Spec: swarm.ServiceSpec{
							Annotations: swarm.Annotations{Name: "service-1"},
							Mode:        tt.mode,
						},
					}))
				case r.Method == http.MethodPost && r.URL.Path == "/v1.41/services/service-1/update":
					updateCalls++
					require.Equal(t, "7", r.URL.Query().Get("version"))
					require.NoError(t, json.NewDecoder(r.Body).Decode(&updatedSpec))
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Warnings": []string{"updated"}}))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			svc := NewSwarmService(&DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, nil, nil)

			resp, err := svc.ScaleService(ctx, "service-1", replicas)
			if tt.wantErr {
				require.Error(t, err)
				require.True(t, cerrdefs.IsInvalidArgument(err), "expected invalid argument, got %v", err)
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
