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

	"github.com/getarcaneapp/arcane/types/v3"
	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/event"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
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

	dockerService := docker.NewDockerClientService(t.Context(), nil, nil, nil).WithClient(dockerClient)
	volumeService := volume.NewVolumeService(db, dockerService, event.NewEventService(db, nil, nil), nil, nil, nil, nil, nil, nil, nil)
	service := NewService(time.UTC, dockerService, nil, nil, volumeService, nil)

	result, err := service.pruneVolumes(t.Context(), types.PruneVolumesOptions{Mode: types.PruneVolumeModeAll})
	require.NoError(t, err)
	require.Equal(t, []string{"orphan"}, result.VolumesDeleted)
	require.EqualValues(t, 100, result.VolumeSpaceReclaimed)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"orphan"}, deleted)
}
