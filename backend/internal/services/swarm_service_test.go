package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
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
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

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

	// Test with additional files
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		Files: []swarmtypes.SyncFile{
			{RelativePath: "config/nginx.conf", Content: []byte("worker_processes 1;")},
			{RelativePath: "scripts/setup.sh", Content: []byte("#!/bin/sh")},
		},
	})
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "config", "nginx.conf"))
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "scripts", "setup.sh"))

	source, err = svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Len(t, source.Files, 2)
}

func TestSwarmService_getPathMapperInternal(t *testing.T) {
	ctx := context.Background()
	db := setupSettingsTestDB(t)
	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(nil, settingsSvc, nil, nil, nil)

	t.Run("returns nil when paths match", func(t *testing.T) {
		pm := svc.getPathMapperInternal(ctx)
		require.Nil(t, pm) // Default is /app/data/swarm/sources which matches itself
	})

	t.Run("returns mapper when mapping configured", func(t *testing.T) {
		containerDir := filepath.Join(t.TempDir(), "container")
		hostDir := "/host/path"
		err := settingsSvc.UpdateSetting(ctx, "swarmStackSourcesDirectory", containerDir+":"+hostDir)
		require.NoError(t, err)

		pm := svc.getPathMapperInternal(ctx)
		require.NotNil(t, pm)
		require.True(t, pm.IsNonMatchingMount())

		testFile := filepath.Join(containerDir, "0/stack/compose.yaml")
		expected := filepath.Join(hostDir, "0/stack/compose.yaml")

		translated, err := pm.ContainerToHost(testFile)
		require.NoError(t, err)
		require.Equal(t, filepath.ToSlash(expected), filepath.ToSlash(translated))
	})
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

// TestSwarmService_BuildNodeAgentStatusInternal covers the state classification.
// The regression case is a poll-mode agent: its persisted env.Status never leaves
// "pending" (HandlePoll only updates the in-memory poll registry), so a fresh
// lastPollAt must still resolve to "connected" rather than "pending".
func TestSwarmService_BuildNodeAgentStatusInternal(t *testing.T) {
	const nodeID = "node-abc"
	now := time.Now()
	svc := &SwarmService{}

	tests := []struct {
		name    string
		env     *models.Environment
		runtime swarmNodeAgentRuntime
		want    swarmtypes.NodeAgentState
	}{
		{
			name:    "poll-mode check-in reports connected despite stale pending status",
			env:     &models.Environment{Status: string(models.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{lastPollAt: &now},
			want:    swarmtypes.NodeAgentStateConnected,
		},
		{
			name:    "no runtime activity and never paired stays pending",
			env:     &models.Environment{Status: string(models.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{},
			want:    swarmtypes.NodeAgentStatePending,
		},
		{
			name:    "tunnel with mismatched identity reports mismatched",
			env:     &models.Environment{Status: string(models.EnvironmentStatusOnline)},
			runtime: swarmNodeAgentRuntime{connected: true, identity: &SwarmNodeIdentity{SwarmNodeID: "other-node", SwarmActive: true}},
			want:    swarmtypes.NodeAgentStateMismatched,
		},
		{
			name:    "tunnel connected without identity probe reports connected",
			env:     &models.Environment{Status: string(models.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{connected: true},
			want:    swarmtypes.NodeAgentStateConnected,
		},
		{
			name:    "previously seen agent with no activity reports offline",
			env:     &models.Environment{Status: string(models.EnvironmentStatusOnline), LastSeen: &now},
			runtime: swarmNodeAgentRuntime{},
			want:    swarmtypes.NodeAgentStateOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.buildNodeAgentStatusInternal(nodeID, tt.env, tt.runtime)
			require.Equal(t, tt.want, got.State)
		})
	}
}
