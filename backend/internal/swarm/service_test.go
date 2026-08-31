package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/environment"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	swarmtypes "github.com/getarcaneapp/arcane/types/v2/swarm"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func setupSwarmServiceTestDBInternal(t *testing.T) *database.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&settings.SettingVariable{}, &environment.Environment{}))
	return &database.DB{DB: db}
}

func newSettingsServiceForSwarmTestInternal(t testing.TB, ctx context.Context, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(t.Context(), lifecycle)
	require.NoError(t, err)
	writes, err := actors.NewExecutor(t.Context(), runtime, "swarm-settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(t.Context(), runtime, "swarm-settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, writes.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, writes, effects)
}

func createSwarmTestEnvironmentInternal(t *testing.T, db *database.DB, id, apiURL, status string, isEdge bool, accessToken *string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&environment.Environment{
		ID: id, CreatedAt: now, UpdatedAt: &now,
		Name:        "env-" + id,
		ApiUrl:      apiURL,
		Status:      status,
		Enabled:     true,
		IsEdge:      isEdge,
		AccessToken: accessToken,
	}).Error)
}

func newSwarmTestDockerClientInternal(t *testing.T, server *httptest.Server) *client.Client {
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
	db := setupSwarmServiceTestDBInternal(t)
	settingsSvc, err := newSettingsServiceForSwarmTestInternal(t, ctx, db)
	require.NoError(t, err)
	envSvc := environment.NewEnvironmentService(db, nil, nil, nil, settingsSvc, nil)

	accessToken := "token-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assert.Equal(t, http.MethodGet, r.Method) {
			return
		}
		if !assert.Equal(t, "/api/swarm/node-identity", r.URL.Path) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-API-Key")) {
			return
		}
		if !assert.Equal(t, accessToken, r.Header.Get("X-Arcane-Agent-Token")) {
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"swarmNodeId":"node-1","hostname":"worker-1","role":"worker","engineVersion":"29.3.1","swarmActive":true}}`))
	}))
	defer server.Close()

	createSwarmTestEnvironmentInternal(
		t,
		db,
		"env-1",
		server.URL,
		string(environment.EnvironmentStatusOnline),
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

type stackSourceDeployRecorder struct {
	calls   int
	lastReq swarmtypes.StackDeployRequest
}

func stubStackSourceUpdateDeployInternal(t *testing.T) *stackSourceDeployRecorder {
	t.Helper()
	original := deployStackAfterSourceUpdateInternal
	t.Cleanup(func() { deployStackAfterSourceUpdateInternal = original })
	rec := &stackSourceDeployRecorder{}
	deployStackAfterSourceUpdateInternal = func(_ *SwarmService, _ context.Context, _ string, req swarmtypes.StackDeployRequest) (*swarmtypes.StackDeployResponse, error) {
		rec.calls++
		rec.lastReq = req
		return &swarmtypes.StackDeployResponse{Name: req.Name}, nil
	}
	return rec
}

func TestSwarmService_UpdateAndGetStackSource_UsesStoredFilesWithoutSwarmManager(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmServiceTestDBInternal(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := newSettingsServiceForSwarmTestInternal(t, ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(nil, settingsSvc, nil, nil, nil)
	deployRec := stubStackSourceUpdateDeployInternal(t)

	updated, err := svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		EnvContent:     "FOO=bar\n",
	})
	require.NoError(t, err)
	require.Equal(t, "demo-stack", updated.Name)
	// Saving stack source must trigger an actual stack deploy (#3463).
	require.Equal(t, 1, deployRec.calls)
	// The edit-path redeploy must carry registry auth, the same as Git Sync (#3778):
	// a service added in the edit has no previous spec to fall back on.
	require.True(t, deployRec.lastReq.WithRegistryAuth)

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

	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		Files: []swarmtypes.SyncFile{
			{RelativePath: "config/nginx.conf", Content: []byte("worker_processes auto;")},
		},
	})
	require.NoError(t, err)
	require.NoFileExists(t, filepath.Join(rootDir, "0", "demo-stack", "scripts", "setup.sh"))
	source, err = svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Len(t, source.Files, 1)
	require.Equal(t, []byte("worker_processes auto;"), source.Files[0].Content)
}

func TestSwarmService_UpdateAndGetStackSource_RoundTripsOverride(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmServiceTestDBInternal(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := newSettingsServiceForSwarmTestInternal(t, ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(nil, settingsSvc, nil, nil, nil)
	stubStackSourceUpdateDeployInternal(t)

	overridePath := filepath.Join(rootDir, "0", "demo-stack", "compose.override.yaml")

	updated, err := svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent:  "services:\n  web:\n    image: nginx:alpine\n",
		OverrideContent: "services:\n  web:\n    image: busybox:latest\n",
	})
	require.NoError(t, err)
	require.Equal(t, "services:\n  web:\n    image: busybox:latest\n", updated.OverrideContent)
	require.FileExists(t, overridePath)

	source, err := svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, updated.OverrideContent, source.OverrideContent)
	// The override is a first-class field, never surfaced as an extra file.
	require.Empty(t, source.Files)

	// Clearing the override removes the file so a UI redeploy stops merging it.
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
	})
	require.NoError(t, err)
	require.NoFileExists(t, overridePath)

	source, err = svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Empty(t, source.OverrideContent)
}

func TestSwarmService_UpdateStackSource_PrunesAndRestoresOnDeployFailure(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmServiceTestDBInternal(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := newSettingsServiceForSwarmTestInternal(t, ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(nil, settingsSvc, nil, nil, nil)

	original := deployStackAfterSourceUpdateInternal
	t.Cleanup(func() { deployStackAfterSourceUpdateInternal = original })
	var lastReq swarmtypes.StackDeployRequest
	var deployErr error
	deployStackAfterSourceUpdateInternal = func(_ *SwarmService, _ context.Context, _ string, req swarmtypes.StackDeployRequest) (*swarmtypes.StackDeployResponse, error) {
		lastReq = req
		if deployErr != nil {
			return nil, deployErr
		}
		return &swarmtypes.StackDeployResponse{Name: req.Name}, nil
	}

	firstCompose := "services:\n  web:\n    image: nginx:alpine\n"
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: firstCompose,
	})
	require.NoError(t, err)
	// The saved source is the full stack spec: services removed from it must
	// be pruned from the swarm on redeploy.
	require.True(t, lastReq.Prune)

	deployErr = errors.New("deploy failed")
	_, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:broken\n",
	})
	require.ErrorContains(t, err, "deploy failed")

	// A failed deploy must not commit the edited source.
	source, err := svc.GetStackSource(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, firstCompose, source.ComposeContent)
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

			svc := NewSwarmService(&docker.DockerClientService{Client: newSwarmTestDockerClientInternal(t, server)}, nil, nil, nil, nil)

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
		env     *environment.Environment
		runtime swarmNodeAgentRuntime
		want    swarmtypes.NodeAgentState
	}{
		{
			name:    "poll-mode check-in reports connected despite stale pending status",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{lastPollAt: &now},
			want:    swarmtypes.NodeAgentStateConnected,
		},
		{
			name:    "no runtime activity and never paired stays pending",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{},
			want:    swarmtypes.NodeAgentStatePending,
		},
		{
			name:    "tunnel with mismatched identity reports mismatched",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusOnline)},
			runtime: swarmNodeAgentRuntime{connected: true, identity: &SwarmNodeIdentity{SwarmNodeID: "other-node", SwarmActive: true}},
			want:    swarmtypes.NodeAgentStateMismatched,
		},
		{
			name:    "tunnel connected without identity probe reports connected",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusPending)},
			runtime: swarmNodeAgentRuntime{connected: true},
			want:    swarmtypes.NodeAgentStateConnected,
		},
		{
			name:    "previously seen agent with no activity reports offline",
			env:     &environment.Environment{Status: string(environment.EnvironmentStatusOnline), LastSeen: &now},
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
