package backup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/getarcaneapp/arcane/types/v2/user"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	containerdomain "github.com/getarcaneapp/arcane/backend/v2/internal/container"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
)

func TestResolveBackupStorageMountFromMounts(t *testing.T) {
	tests := []struct {
		name         string
		mounts       []container.MountPoint
		target       string
		readOnly     bool
		wantResolved bool
		wantType     mount.Type
		wantSource   string
		wantTarget   string
		wantReadOnly bool
	}{
		{
			name: "mirrors bind mount",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			target:       "/volume",
			readOnly:     true,
			wantResolved: true,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: true,
		},
		{
			name: "writable request against read-only bind mount still resolves",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups", RW: false},
			},
			target:       "/volume",
			readOnly:     false,
			wantResolved: true,
			wantType:     mount.TypeBind,
			wantSource:   "/host/backups",
			wantTarget:   "/volume",
			wantReadOnly: false,
		},
		{
			name: "mirrors named volume",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     false,
			wantResolved: true,
			wantType:     mount.TypeVolume,
			wantSource:   "arcane-backups",
			wantTarget:   "/backups",
			wantReadOnly: false,
		},
		{
			name: "ignores unsupported mount types",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
		{
			name:         "returns unresolved when mount is absent",
			target:       "/backups",
			readOnly:     true,
			wantResolved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBackupStorageMountFromMounts(t.Context(), tt.mounts, tt.target, tt.readOnly)
			require.Equal(t, tt.wantResolved, got != nil)
			if !tt.wantResolved {
				return
			}

			require.Equal(t, tt.wantType, got.Type)
			require.Equal(t, tt.wantSource, got.Source)
			require.Equal(t, tt.wantTarget, got.Target)
			require.Equal(t, tt.wantReadOnly, got.ReadOnly)
		})
	}
}

func TestBackupMountWarningFromArcaneMounts(t *testing.T) {
	tests := []struct {
		name   string
		mounts []container.MountPoint
		want   string
	}{
		{
			name: "bind mount at backups suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "named volume at backups suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "arcane-backups", Destination: "/backups"},
			},
			want: "",
		},
		{
			name: "bind mount at restores suppresses warning",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/restores", Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "unsupported restores mount still suppresses warning for compatibility",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/restores"},
			},
			want: "",
		},
		{
			name: "missing backups mount warns",
			mounts: []container.MountPoint{
				{Type: mount.TypeBind, Source: "/host/other", Destination: "/other"},
			},
			want: backupMountMissingWarning,
		},
		{
			name: "unsupported backups mount type warns",
			mounts: []container.MountPoint{
				{Type: mount.TypeTmpfs, Destination: "/backups"},
			},
			want: backupMountMissingWarning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, backupMountWarningFromArcaneMounts(tt.mounts))
		})
	}
}

func setupVolumeBackupLifecycleTest(t *testing.T, handler http.Handler) (*Service, *client.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&event.Event{}))
	db := &database.DB{DB: gormDB}
	dockerService := docker.NewDockerClientService(t.Context(), db, &config.Config{}, nil).WithClient(dockerClient)
	eventService := event.NewEventService(db, &config.Config{}, nil)
	containerService := containerdomain.NewContainerService(eventService, dockerService, nil, nil, nil)
	return NewService(Dependencies{StopContainer: containerService.StopContainer, StartContainer: containerService.StartContainer}), dockerClient
}

func TestVolumeBackupContainerLifecycleStopsAndRestartsOnlyRunningContainersUsingVolume(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{ID: "uses-volume", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
				{ID: "other-volume", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "other-data"}}},
				{ID: "arcane", Labels: map[string]string{"com.getarcaneapp.arcane": "true"}, Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/uses-volume/stop"):
			mu.Lock()
			operations = append(operations, "stop:uses-volume")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/uses-volume/start"):
			mu.Lock()
			operations = append(operations, "start:uses-volume")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	service, dockerClient := setupVolumeBackupLifecycleTest(t, serverHandler)
	actor := user.Actor{ID: "user-1", Username: "tester"}
	stopped, err := service.stopRunningContainersForBackup(t.Context(), dockerClient, "app-data", actor, false)
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	require.Equal(t, "uses-volume", stopped[0].ID)

	remaining, err := service.startContainersAfterBackup(t.Context(), dockerClient, stopped, actor)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, []string{"stop:uses-volume", "start:uses-volume"}, operations)
}

func TestVolumeBackupContainerLifecycleRollsBackStoppedContainersOnStopFailure(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{ID: "first", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
				{ID: "second", Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}}},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/first/stop"):
			mu.Lock()
			operations = append(operations, "stop:first")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/containers/second/stop"):
			mu.Lock()
			operations = append(operations, "stop:second")
			mu.Unlock()
			http.Error(w, "stop failed", http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/containers/first/start"):
			mu.Lock()
			operations = append(operations, "start:first")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	service, dockerClient := setupVolumeBackupLifecycleTest(t, serverHandler)
	actor := user.Actor{ID: "user-1", Username: "tester"}
	stillStopped, err := service.stopRunningContainersForBackup(t.Context(), dockerClient, "app-data", actor, false)
	require.ErrorContains(t, err, "failed to stop container second")
	require.Empty(t, stillStopped)
	require.Equal(t, []string{"stop:first", "stop:second", "start:first"}, operations)
}

func TestVolumeBackupContainerLifecycleWaitsForRunningComposeReplacement(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	listCalls := 0
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			mu.Lock()
			listCalls++
			call := listCalls
			mu.Unlock()
			if call == 1 {
				assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
					{
						ID:     "old-id",
						Names:  []string{"/old-name"},
						State:  container.StateRunning,
						Labels: map[string]string{"com.docker.compose.project": "vault", "com.docker.compose.service": "vaultwarden", "com.docker.compose.container-number": "1"},
						Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}},
					},
				}))
				return
			}
			if call == 2 {
				assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{}))
				return
			}
			assert.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
				{
					ID:     "new-id",
					Names:  []string{"/new-name"},
					State:  container.StateRunning,
					Labels: map[string]string{"com.docker.compose.project": "vault", "com.docker.compose.service": "vaultwarden", "com.docker.compose.container-number": "1"},
					Mounts: []container.MountPoint{{Type: mount.TypeVolume, Name: "app-data"}},
				},
			}))
		case strings.HasSuffix(r.URL.Path, "/containers/old-id/stop"):
			mu.Lock()
			operations = append(operations, "stop:old-id")
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case strings.Contains(r.URL.Path, "/start"):
			mu.Lock()
			operations = append(operations, "unexpected:"+r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	service, dockerClient := setupVolumeBackupLifecycleTest(t, serverHandler)
	actor := user.Actor{ID: "user-1", Username: "tester"}
	stopped, err := service.stopRunningContainersForBackup(t.Context(), dockerClient, "app-data", actor, false)
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	require.Equal(t, "old-id", stopped[0].ID)

	remaining, err := service.startContainersAfterBackup(t.Context(), dockerClient, stopped, actor)
	require.NoError(t, err)
	require.Empty(t, remaining)
	require.Equal(t, []string{"stop:old-id"}, operations)
}
