package prune

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libtnb/sqlite"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"getarcane.dev/arcane/v3/internal/activity"
	"getarcane.dev/arcane/v3/internal/backup"
	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/event"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/internal/volume"
	"getarcane.dev/arcane/v3/pkg/httpx"
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
		Policies:      backup.NewPolicies(db),
		DockerService: dockerService,
		EventService:  event.NewEventService(db, &config.Config{}, nil),
		Repository:    backup.NewVolumeRepository(backup.VolumeRepositoryDependencies{Docker: dockerService, BackupVolumeName: "arcane-backups"}),
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
	var mu sync.Mutex
	var containerQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/system/df"):
			if slices.Contains(r.URL.Query()["type"], "container") {
				http.Error(w, "unexpected container disk usage request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{`+
				`"Images":[`+
				`{"Id":"sha256:unused","Containers":0,"Size":500,"SharedSize":100},`+
				`{"Id":"sha256:used","Containers":1,"Size":900,"SharedSize":0}],`+
				`"Volumes":[`+
				`{"Name":"orphan","Driver":"local","UsageData":{"RefCount":0,"Size":100}},`+
				`{"Name":"mounted","Driver":"local","UsageData":{"RefCount":1,"Size":50}},`+
				`{"Name":"arcane-backups","Driver":"local","UsageData":{"RefCount":0,"Size":200}}],`+
				`"BuildCache":[`+
				`{"ID":"idle","InUse":false,"Size":10},`+
				`{"ID":"busy","InUse":true,"Size":20}]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/containers/json"):
			// Finding the volume that holds backups lists Arcane's own container, so only the stopped-container listing is kept.
			if strings.Contains(r.URL.Query().Get("filters"), "status") {
				mu.Lock()
				containerQuery = r.URL.Query()
				mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `[{"Id":"stopped","State":"exited","SizeRw":30}]`)
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
		Policies:      backup.NewPolicies(db),
		DockerService: dockerService,
		EventService:  event.NewEventService(db, &config.Config{}, nil),
		Repository:    backup.NewVolumeRepository(backup.VolumeRepositoryDependencies{Docker: dockerService, BackupVolumeName: "arcane-backups"}),
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
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "1", containerQuery.Get("all"))
	require.Equal(t, "1", containerQuery.Get("size"))
	var filters client.Filters
	require.NoError(t, json.Unmarshal([]byte(containerQuery.Get("filters")), &filters))
	require.Equal(t, client.Filters{
		"status": {
			"created":  true,
			"removing": true,
			"exited":   true,
			"dead":     true,
		},
	}, filters)
}

func newSettingsDockerService(t *testing.T, dockerClient *client.Client) *docker.DockerClientService {
	t.Helper()

	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&settings.SettingVariable{}))
	db := &database.DB{DB: gormDB}
	settingsService, err := settings.NewSettingsService(settings.SettingsServiceParams{
		Ctx:    t.Context(),
		DB:     db,
		Config: &config.Config{},
		Env:    settings.Env{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, settingsService.Stop(context.WithoutCancel(t.Context()))) })
	return docker.NewDockerClientService(t.Context(), db, settingsService, docker.ClientServiceOptions{}).WithClient(dockerClient)
}

func TestPreparePrunePlanRequestsOnlySelectedUsageTypes(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	service := newPruneTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/system/df") {
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		mu.Lock()
		requested = r.URL.Query()["type"]
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"Images":[`+
			`{"Id":"sha256:dangling","Containers":0,"Size":500,"SharedSize":0},`+
			`{"Id":"sha256:tagged","Containers":0,"Size":900,"SharedSize":0,"RepoTags":["app:latest"]}]}`)
	}))

	plan, err := service.preparePrunePlan(t.Context(), types.PruneAllRequest{
		Images: &types.PruneImagesOptions{Mode: types.PruneImageModeDangling},
	})
	require.NoError(t, err)
	require.Equal(t, []pruneResource{{Kind: "image", ID: "sha256:dangling", Size: 500}}, plan.Resources)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"image"}, requested)
}

func TestPreparePrunePlanMarksDiskUsageFailure(t *testing.T) {
	service := newPruneTestService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "df failed", http.StatusInternalServerError)
	}))

	_, err := service.preparePrunePlan(t.Context(), types.PruneAllRequest{
		Images: &types.PruneImagesOptions{Mode: types.PruneImageModeDangling},
	})
	require.ErrorIs(t, err, errDiskUsageUnavailable)
}

func newPruneTestService(t *testing.T, handler http.Handler) *Service {
	t.Helper()

	server := httptest.NewServer(handler)
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

	return NewService(ServiceParams{
		Location:      time.UTC,
		DockerService: newSettingsDockerService(t, dockerClient),
	})
}

func TestPruneVolumesFallsBackToVolumeListWithoutDiskUsage(t *testing.T) {
	service, deleted := newVolumeFallbackService(t)

	result, err := service.pruneVolumes(t.Context(), types.PruneVolumesOptions{Mode: types.PruneVolumeModeAll})
	require.NoError(t, err)
	require.Equal(t, []string{"orphan"}, result.VolumesDeleted)
	require.Equal(t, []string{"orphan"}, deleted())
}

func TestReconcileScheduledPruneReportsInterruptedFallbackWithoutReplaying(t *testing.T) {
	service, deleted := newVolumeFallbackService(t)

	outcome, err := service.ReconcileScheduledPrune(t.Context(), types.SchedulerRun{
		EnvironmentID: "0",
		Outcome: types.SchedulerOutcome{
			Targets: []types.TargetOutcome{{
				ResourceType: "prune_fallback",
				ID:           "prune-fallback",
				Status:       types.RunStatusRunning,
				ActivityID:   "activity-1",
			}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, types.RunStatusFailed, outcome.Status)
	require.Equal(t, "activity-1", outcome.ActivityID)
	require.Empty(t, deleted())
}

func newVolumeFallbackService(t *testing.T) (*Service, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/system/df"):
			http.Error(w, "df failed", http.StatusInternalServerError)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/volumes"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"Volumes":[`+
				`{"Name":"orphan","Driver":"local"},`+
				`{"Name":"arcane-backups","Driver":"local"},`+
				`{"Name":"helper","Driver":"local","Labels":{"com.getarcaneapp.internal.resource":"true"}}]}`)
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
	require.NoError(t, gormDB.AutoMigrate(&event.Event{}, &activity.Activity{}, &activity.ActivityMessage{}))
	db := &database.DB{DB: gormDB}

	dockerService := newSettingsDockerService(t, dockerClient)
	service := NewService(ServiceParams{
		Location:        time.UTC,
		DockerService:   dockerService,
		ActivityService: activity.NewActivityService(db, nil),
		VolumeService: volume.NewVolumeService(volume.VolumeServiceParams{
			Config:        &config.Config{},
			DB:            db,
			Policies:      backup.NewPolicies(db),
			DockerService: dockerService,
			EventService:  event.NewEventService(db, &config.Config{}, nil),
			Repository:    backup.NewVolumeRepository(backup.VolumeRepositoryDependencies{Docker: dockerService, BackupVolumeName: "arcane-backups"}),
		}),
	})
	return service, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(deleted)
	}
}

func diskUsageScopeContext(t *testing.T, bodies map[string]any) context.Context {
	t.Helper()
	return httpx.WithEnvironmentScope(t.Context(), &httpx.EnvironmentScope{
		IDs:     []string{"a", "b", "c"},
		Request: httptest.NewRequest(http.MethodGet, "/api/agents/a,b,c/system/df", http.NoBody),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, ok := bodies[strings.Split(strings.TrimPrefix(r.URL.Path, "/api/agents/"), "/")[0]]
			if !ok {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"detail":"environment offline"}`))
				return
			}
			_ = json.MarshalWrite(w, body)
		}),
		Resolve: func(_ context.Context, id string) (types.EnvironmentRef, error) {
			return types.EnvironmentRef{ID: id, Name: "Env " + id}, nil
		},
	})
}

func TestAggregateDiskUsageSumsEnvironments(t *testing.T) {
	ctx := diskUsageScopeContext(t, map[string]any{
		"a": types.ApiResponse[types.SystemDiskUsage]{Success: true, Data: types.SystemDiskUsage{
			UnusedImagesBytes:      1,
			BuildCacheBytes:        2,
			UnusedVolumesBytes:     3,
			StoppedContainersBytes: 4,
		}},
		"b": types.ApiResponse[types.SystemDiskUsage]{Success: true, Data: types.SystemDiskUsage{
			UnusedImagesBytes:      10,
			BuildCacheBytes:        20,
			UnusedVolumesBytes:     30,
			StoppedContainersBytes: 40,
		}},
	})

	body, err := (&Service{}).AggregateDiskUsage(ctx)
	require.NoError(t, err)

	require.True(t, body.Success)
	require.Equal(t, types.SystemDiskUsage{
		UnusedImagesBytes:      11,
		BuildCacheBytes:        22,
		UnusedVolumesBytes:     33,
		StoppedContainersBytes: 44,
	}, body.Data)
	require.Len(t, body.Environments, 2)
	require.Equal(t, []types.EnvironmentFailure{{Environment: types.EnvironmentRef{ID: "c", Name: "c"}, Message: "environment offline"}}, body.FailedEnvironments)
}
