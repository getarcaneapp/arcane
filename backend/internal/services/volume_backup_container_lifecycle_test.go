package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	sqlite "github.com/libtnb/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVolumeBackupLifecycleTestInternal(t *testing.T, handler http.Handler) (*VolumeService, *client.Client) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dockerClient, err := client.New(client.WithHost(server.URL), client.WithAPIVersion("1.41"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	gormDB, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.Event{}))
	db := &database.DB{DB: gormDB}
	dockerService := &DockerClientService{client: dockerClient}
	eventService := NewEventService(db, &config.Config{}, nil)
	containerService := &ContainerService{db: db, dockerService: dockerService, eventService: eventService}
	return &VolumeService{containerService: containerService}, dockerClient
}

func TestVolumeBackupContainerLifecycleStopsAndRestartsOnlyRunningContainersUsingVolume(t *testing.T) {
	var mu sync.Mutex
	var operations []string
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
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

	service, dockerClient := setupVolumeBackupLifecycleTestInternal(t, serverHandler)
	actor := models.User{BaseModel: models.BaseModel{ID: "user-1"}, Username: "tester"}
	stopped, err := service.stopRunningContainersForBackupInternal(context.Background(), dockerClient, "app-data", actor)
	require.NoError(t, err)
	require.Equal(t, []string{"uses-volume"}, stopped)

	remaining, err := service.startContainersAfterBackupInternal(context.Background(), stopped, actor)
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
			require.NoError(t, json.NewEncoder(w).Encode([]container.Summary{
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

	service, dockerClient := setupVolumeBackupLifecycleTestInternal(t, serverHandler)
	actor := models.User{BaseModel: models.BaseModel{ID: "user-1"}, Username: "tester"}
	stillStopped, err := service.stopRunningContainersForBackupInternal(context.Background(), dockerClient, "app-data", actor)
	require.ErrorContains(t, err, "failed to stop container second")
	require.Empty(t, stillStopped)
	require.Equal(t, []string{"stop:first", "stop:second", "start:first"}, operations)
}
