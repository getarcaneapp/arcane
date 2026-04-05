package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/pkg/pagination"
	swarmtypes "github.com/getarcaneapp/arcane/types/swarm"
	dockerswarm "github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/api/types/system"
	dockerclient "github.com/moby/moby/client"
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

	svc := NewSwarmService(db, nil, nil, nil, nil, envSvc)

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
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(db, nil, settingsSvc, nil, nil, nil)

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
	updated, err = svc.UpdateStackSource(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
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

	svc := NewSwarmService(nil, nil, settingsSvc, nil, nil, nil)

	t.Run("returns nil when paths match", func(t *testing.T) {
		pm, err := svc.getPathMapperInternal(ctx)
		require.NoError(t, err)
		require.Nil(t, pm) // Default is /app/data/swarm/sources which matches itself
	})

	t.Run("returns mapper when mapping configured", func(t *testing.T) {
		containerDir := filepath.Join(t.TempDir(), "container")
		hostDir := "/host/path"
		err := settingsSvc.UpdateSetting(ctx, "swarmStackSourcesDirectory", containerDir+":"+hostDir)
		require.NoError(t, err)

		pm, err := svc.getPathMapperInternal(ctx)
		require.NoError(t, err)
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
		mode       dockerswarm.ServiceMode
		assertMode func(*testing.T, dockerswarm.ServiceMode)
		wantErr    bool
	}{
		{
			name: "replicated",
			mode: dockerswarm.ServiceMode{Replicated: &dockerswarm.ReplicatedService{}},
			assertMode: func(t *testing.T, mode dockerswarm.ServiceMode) {
				t.Helper()
				require.NotNil(t, mode.Replicated)
				require.NotNil(t, mode.Replicated.Replicas)
				require.Equal(t, replicas, *mode.Replicated.Replicas)
				require.Nil(t, mode.ReplicatedJob)
			},
		},
		{
			name: "replicated job",
			mode: dockerswarm.ServiceMode{ReplicatedJob: &dockerswarm.ReplicatedJob{MaxConcurrent: &maxConcurrent}},
			assertMode: func(t *testing.T, mode dockerswarm.ServiceMode) {
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
			mode:    dockerswarm.ServiceMode{Global: &dockerswarm.GlobalService{}},
			wantErr: true,
		},
		{
			name:    "global job",
			mode:    dockerswarm.ServiceMode{GlobalJob: &dockerswarm.GlobalJob{}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateCalls := 0
			var updatedSpec dockerswarm.ServiceSpec

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1.41/info":
					require.NoError(t, json.NewEncoder(w).Encode(system.Info{
						Swarm: dockerswarm.Info{
							LocalNodeState:   dockerswarm.LocalNodeStateActive,
							ControlAvailable: true,
						},
					}))
				case r.Method == http.MethodGet && r.URL.Path == "/v1.41/services/service-1":
					require.NoError(t, json.NewEncoder(w).Encode(dockerswarm.Service{
						ID: "service-1",
						Meta: dockerswarm.Meta{
							Version: dockerswarm.Version{Index: 7},
						},
						Spec: dockerswarm.ServiceSpec{
							Annotations: dockerswarm.Annotations{Name: "service-1"},
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

			svc := NewSwarmService(nil, &DockerClientService{client: newTestDockerClient(t, server)}, nil, nil, nil, nil)

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

func TestSwarmService_GetStackProject_UsesStoredFilesWithoutDocker(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(db, nil, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		EnvContent:     "FOO=bar\n",
	})
	require.NoError(t, err)

	stackProject, err := svc.GetStackProject(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, "demo-stack", stackProject.Name)
	require.Equal(t, swarmtypes.StackProjectRuntimeStateUnavailable, stackProject.RuntimeState)
	require.Equal(t, 1, stackProject.ServiceCount)
	require.Contains(t, stackProject.ComposeContent, "nginx:alpine")
	require.Equal(t, "FOO=bar\n", stackProject.EnvContent)

	counts, err := svc.GetStackProjectStatusCounts(ctx, "0")
	require.NoError(t, err)
	require.Equal(t, 1, counts.TotalStackProjects)
	require.Equal(t, 0, counts.LiveStackProjects)
	require.Equal(t, 0, counts.DownStackProjects)
	require.Equal(t, 1, counts.UnavailableStackProjects)
}

func TestSwarmService_GetStackProject_LoadsDiskStateIntoDatabase(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	stackDir := filepath.Join(rootDir, "0", "demo-stack")
	require.NoError(t, os.MkdirAll(stackDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stackDir, "compose.yaml"), []byte("services:\n  web:\n    image: nginx:alpine\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(stackDir, ".env"), []byte("FOO=bar\n"), 0o644))

	svc := NewSwarmService(db, nil, settingsSvc, nil, nil, nil)

	stackProject, err := svc.GetStackProject(ctx, "0", "demo-stack")
	require.NoError(t, err)
	require.Equal(t, "demo-stack", stackProject.Name)
	require.Equal(t, 1, stackProject.ServiceCount)
	require.Equal(t, "FOO=bar\n", stackProject.EnvContent)

	var persisted models.SwarmStackProject
	require.NoError(t, db.WithContext(ctx).Where("environment_id = ? AND name = ?", "0", "demo-stack").First(&persisted).Error)
	require.Equal(t, filepath.Join(rootDir, "0", "demo-stack"), persisted.Path)
	require.Equal(t, 1, persisted.ServiceCount)
}

func TestSwarmService_ListStackProjectsPaginated_UsesStoredComposeServiceCountWhenDown(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	dockerSvc := newSwarmTestDockerService(t, settingsSvc, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/services"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	svc := NewSwarmService(db, dockerSvc, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n  worker:\n    image: busybox\n",
	})
	require.NoError(t, err)

	items, paginationResp, err := svc.ListStackProjectsPaginated(ctx, "0", pagination.QueryParams{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, 1, paginationResp.TotalItems)
	require.Equal(t, swarmtypes.StackProjectRuntimeStateDown, items[0].RuntimeState)
	require.Equal(t, 2, items[0].ServiceCount)
}

func TestSwarmService_UpsertStackProject_RenamesStoppedProject(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	svc := NewSwarmService(db, nil, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		EnvContent:     "FOO=bar\n",
	})
	require.NoError(t, err)

	renamed, err := svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		Name:           "renamed-stack",
		ComposeContent: "services:\n  web:\n    image: nginx:stable-alpine\n",
		EnvContent:     "FOO=baz\n",
	})
	require.NoError(t, err)
	require.Equal(t, "renamed-stack", renamed.Name)
	require.Equal(t, "FOO=baz\n", renamed.EnvContent)

	require.NoFileExists(t, filepath.Join(rootDir, "0", "demo-stack", "compose.yaml"))
	require.FileExists(t, filepath.Join(rootDir, "0", "renamed-stack", "compose.yaml"))

	_, err = svc.GetStackProject(ctx, "0", "demo-stack")
	require.True(t, cerrdefs.IsNotFound(err))

	var persisted models.SwarmStackProject
	require.NoError(t, db.WithContext(ctx).Where("environment_id = ? AND name = ?", "0", "renamed-stack").First(&persisted).Error)
	require.Equal(t, filepath.Join(rootDir, "0", "renamed-stack"), persisted.Path)
}

func TestSwarmService_ListStacksPaginated_ExcludesSavedOnlyStackProjects(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	dockerSvc := newSwarmTestDockerService(t, settingsSvc, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/info"):
			_, _ = w.Write([]byte(`{"Swarm":{"LocalNodeState":"active","ControlAvailable":true}}`))
		case strings.HasSuffix(r.URL.Path, "/services"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	svc := NewSwarmService(db, dockerSvc, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
	})
	require.NoError(t, err)

	items, paginationResp, err := svc.ListStacksPaginated(ctx, "0", pagination.QueryParams{})
	require.NoError(t, err)
	require.Empty(t, items)
	require.EqualValues(t, 0, paginationResp.TotalItems)
}

func TestSwarmService_DeleteStackProject_ReturnsConflictWhenStackIsLive(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	dockerSvc := newSwarmTestDockerService(t, settingsSvc, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/services"):
			require.NoError(t, json.NewEncoder(w).Encode([]dockerswarm.Service{
				{
					ID: "service-1",
					Spec: dockerswarm.ServiceSpec{
						Annotations: dockerswarm.Annotations{
							Name: "demo-stack_web",
							Labels: map[string]string{
								swarmtypes.StackNamespaceLabel: "demo-stack",
							},
						},
					},
				},
			}))
		default:
			http.NotFound(w, r)
		}
	})

	svc := NewSwarmService(db, dockerSvc, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
	})
	require.NoError(t, err)

	err = svc.DeleteStackProject(ctx, "0", "demo-stack")
	require.Error(t, err)
	require.True(t, cerrdefs.IsConflict(err))
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "compose.yaml"))
}

func TestSwarmService_DownStack_PreservesSavedFiles(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	serviceRemoved := false
	dockerSvc := newSwarmTestDockerService(t, settingsSvc, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/info"):
			_, _ = w.Write([]byte(`{"Swarm":{"LocalNodeState":"active","ControlAvailable":true}}`))
		case strings.HasSuffix(r.URL.Path, "/services") && r.Method == http.MethodGet:
			require.NoError(t, json.NewEncoder(w).Encode([]dockerswarm.Service{
				{
					ID: "service-1",
					Spec: dockerswarm.ServiceSpec{
						Annotations: dockerswarm.Annotations{
							Name: "demo-stack_web",
							Labels: map[string]string{
								swarmtypes.StackNamespaceLabel: "demo-stack",
							},
						},
					},
				},
			}))
		case strings.Contains(r.URL.Path, "/services/service-1") && r.Method == http.MethodDelete:
			serviceRemoved = true
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/configs"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/secrets"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	svc := NewSwarmService(db, dockerSvc, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
		EnvContent:     "FOO=bar\n",
	})
	require.NoError(t, err)

	err = svc.DownStack(ctx, "demo-stack")
	require.NoError(t, err)
	require.True(t, serviceRemoved)
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "compose.yaml"))

	envContent, err := os.ReadFile(filepath.Join(rootDir, "0", "demo-stack", ".env"))
	require.NoError(t, err)
	require.Equal(t, "FOO=bar\n", string(envContent))
}

func TestSwarmService_DownStack_IgnoresTaskListServiceNotFoundAfterRemoval(t *testing.T) {
	ctx := context.Background()
	db := setupSwarmTestDB(t)
	rootDir := t.TempDir()
	t.Setenv("SWARM_STACK_SOURCES_DIRECTORY", rootDir)

	settingsSvc, err := NewSettingsService(ctx, db)
	require.NoError(t, err)

	serviceRemoved := false
	dockerSvc := newSwarmTestDockerService(t, settingsSvc, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/info"):
			_, _ = w.Write([]byte(`{"Swarm":{"LocalNodeState":"active","ControlAvailable":true}}`))
		case strings.HasSuffix(r.URL.Path, "/services") && r.Method == http.MethodGet:
			require.NoError(t, json.NewEncoder(w).Encode([]dockerswarm.Service{
				{
					ID: "service-1",
					Spec: dockerswarm.ServiceSpec{
						Annotations: dockerswarm.Annotations{
							Name: "demo-stack_web",
							Labels: map[string]string{
								swarmtypes.StackNamespaceLabel: "demo-stack",
							},
						},
					},
				},
			}))
		case strings.Contains(r.URL.Path, "/services/service-1") && r.Method == http.MethodDelete:
			serviceRemoved = true
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			http.Error(w, "Error response from daemon: service service-1 not found", http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/configs"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/secrets"):
			_, _ = w.Write([]byte(`[]`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})

	svc := NewSwarmService(db, dockerSvc, settingsSvc, nil, nil, nil)

	_, err = svc.UpsertStackProject(ctx, "0", "demo-stack", swarmtypes.StackSourceUpdateRequest{
		ComposeContent: "services:\n  web:\n    image: nginx:alpine\n",
	})
	require.NoError(t, err)

	err = svc.DownStack(ctx, "demo-stack")
	require.NoError(t, err)
	require.True(t, serviceRemoved)
	require.FileExists(t, filepath.Join(rootDir, "0", "demo-stack", "compose.yaml"))
}

func newSwarmTestDockerService(
	t *testing.T,
	settingsSvc *SettingsService,
	handler http.HandlerFunc,
) *DockerClientService {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	dockerCli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(server.URL),
		dockerclient.WithVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dockerCli.Close()
	})

	return &DockerClientService{
		client:          dockerCli,
		config:          &config.Config{DockerHost: server.URL},
		settingsService: settingsSvc,
	}
}

func setupSwarmTestDB(t *testing.T) *database.DB {
	t.Helper()

	db := setupSettingsTestDB(t)
	require.NoError(t, db.AutoMigrate(&models.SwarmStackProject{}))

	return db
}
