package prune

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/docker/streams/bus"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/types/v3"
)

func TestPruneVolumesSkipsBackupVolume(t *testing.T) {
	var mu sync.Mutex
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/system/df"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"Volumes":[`+
				`{"Name":"orphan","Driver":"local","UsageData":{"RefCount":0,"Size":100}},`+
				`{"Name":"arcane-backups","Driver":"local","UsageData":{"RefCount":0,"Size":200}}]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[]`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/volumes/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"Name":"`+path.Base(r.URL.Path)+`","Driver":"local"}`)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/volumes/"):
			mu.Lock()
			deleted = append(deleted, path.Base(r.URL.Path))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	dialer := &net.Dialer{}
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
		client.WithDialContext(func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	gormDB, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&event.Event{}))
	db := &database.DB{DB: gormDB}

	dockerService := newSettingsDockerService(t, dockerClient)
	volumeService := volume.NewVolumeService(volume.VolumeServiceParams{
		Config:        &config.Config{},
		DB:            db,
		DockerService: dockerService,
		EventService:  event.NewEventService(db, nil, nil),
	})
	service := NewService(ServiceParams{
		Location:      time.UTC,
		DockerService: dockerService,
		VolumeService: volumeService,
	})

	result, err := service.pruneVolumes(t.Context(), types.PruneVolumesOptions{Mode: types.PruneVolumeModeAll})
	require.NoError(t, err)
	require.Equal(t, []string{"orphan"}, result.VolumesDeleted)
	require.EqualValues(t, 100, result.VolumeSpaceReclaimed)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"orphan"}, deleted)
}

func TestGetDiskUsageSumsReclaimableResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/system/df") {
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{`+
			`"Images":[`+
			`{"Id":"sha256:unused","Containers":0,"Size":500,"SharedSize":100},`+
			`{"Id":"sha256:used","Containers":1,"Size":900,"SharedSize":0}],`+
			`"Containers":[`+
			`{"Id":"stopped","State":"exited","SizeRw":30},`+
			`{"Id":"running","State":"running","SizeRw":70}],`+
			`"Volumes":[`+
			`{"Name":"orphan","Driver":"local","UsageData":{"RefCount":0,"Size":100}},`+
			`{"Name":"mounted","Driver":"local","UsageData":{"RefCount":1,"Size":50}},`+
			`{"Name":"arcane-backups","Driver":"local","UsageData":{"RefCount":0,"Size":200}}],`+
			`"BuildCache":[`+
			`{"ID":"idle","InUse":false,"Size":10},`+
			`{"ID":"busy","InUse":true,"Size":20}]}`)
	}))
	t.Cleanup(server.Close)

	dialer := &net.Dialer{}
	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
		client.WithDialContext(func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

	gormDB, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&event.Event{}))
	db := &database.DB{DB: gormDB}

	dockerService := newSettingsDockerService(t, dockerClient)
	volumeService := volume.NewVolumeService(volume.VolumeServiceParams{
		Config:        &config.Config{},
		DB:            db,
		DockerService: dockerService,
		EventService:  event.NewEventService(db, nil, nil),
	})
	service := NewService(ServiceParams{
		Location:      time.UTC,
		DockerService: dockerService,
		VolumeService: volumeService,
	})

	usage, err := service.GetDiskUsage(t.Context())
	require.NoError(t, err)
	require.Equal(t, types.SystemDiskUsage{
		UnusedImagesBytes:      400,
		BuildCacheBytes:        10,
		UnusedVolumesBytes:     100,
		StoppedContainersBytes: 30,
	}, usage)
}

func newSettingsDockerService(t *testing.T, dockerClient *client.Client) *docker.DockerClientService {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(t.Context(), db, &config.Config{}, settings.Env{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient)
}
