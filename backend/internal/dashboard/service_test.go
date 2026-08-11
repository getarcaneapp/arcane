package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/v2/internal/actors"
	"github.com/getarcaneapp/arcane/backend/v2/internal/config"
	"github.com/getarcaneapp/arcane/backend/v2/internal/database"
	"github.com/getarcaneapp/arcane/backend/v2/internal/docker"
	"github.com/getarcaneapp/arcane/backend/v2/internal/image"
	"github.com/getarcaneapp/arcane/backend/v2/internal/models"
	"github.com/getarcaneapp/arcane/backend/v2/internal/project"
	"github.com/getarcaneapp/arcane/backend/v2/internal/settings"
	"github.com/getarcaneapp/arcane/backend/v2/internal/volume"
	dashboardtypes "github.com/getarcaneapp/arcane/types/v2/dashboard"
	volumetypes "github.com/getarcaneapp/arcane/types/v2/volume"
	"github.com/libtnb/sqlite"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	dockermount "github.com/moby/moby/api/types/mount"
	dockervolume "github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"gorm.io/gorm"
)

func setupDashboardServiceTestDB(t *testing.T) (*database.DB, *settings.SettingsService) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ApiKey{}, &models.Environment{}, &models.ImageUpdateRecord{}, &models.Project{}, &models.SettingVariable{}))

	databaseDB := &database.DB{DB: db}
	settingsSvc, err := newSettingsServiceForTestInternal(context.Background(), t, databaseDB)
	require.NoError(t, err)

	return databaseDB, settingsSvc
}

func createDashboardTestAPIKey(t *testing.T, db *database.DB, key models.ApiKey) {
	t.Helper()
	require.NoError(t, db.WithContext(context.Background()).Create(&key).Error)
}

func createDashboardTestImageUpdateRecord(t *testing.T, db *database.DB, record models.ImageUpdateRecord) {
	t.Helper()
	require.NoError(t, db.WithContext(context.Background()).Create(&record).Error)
}

func newDashboardTestDockerService(
	t *testing.T,
	settingsSvc *settings.SettingsService,
	containers []dockercontainer.Summary,
	images []dockerimage.Summary,
	volumes []dockervolume.Volume,
) *docker.DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			if !assert.NoError(t, json.NewEncoder(w).Encode(containers)) {
				return
			}
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			if !assert.NoError(t, json.NewEncoder(w).Encode(images)) {
				return
			}
		case strings.HasSuffix(r.URL.Path, "/volumes"):
			if !assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"Volumes": volumes, "Warnings": []string{}})) {
				return
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dockerClient.Close()
	})

	return docker.NewDockerClientService(t.Context(), nil, nil, settingsSvc).WithClient(dockerClient)
}

func TestDashboardService_GetSnapshot_ReturnsDashboardSnapshot(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)

	containers := []dockercontainer.Summary{
		{
			ID:      "container-running",
			Names:   []string{"/running-app"},
			Image:   "repo/app:stable",
			ImageID: "sha256:image-a",
			Created: 1700000000,
			State:   "running",
			Status:  "Up 2 hours",
			Labels:  map[string]string{},
			Mounts: []dockercontainer.MountPoint{
				{Type: dockermount.TypeVolume, Name: "app-data"},
			},
		},
		{
			ID:      "container-stopped",
			Names:   []string{"/stopped-app"},
			Image:   "repo/worker:latest",
			ImageID: "sha256:image-b",
			Created: 1800000000,
			State:   "exited",
			Status:  "Exited (0) 1 hour ago",
			Labels:  map[string]string{},
		},
		{
			ID:      "container-internal",
			Names:   []string{"/arcane"},
			Image:   "ghcr.io/getarcaneapp/arcane:latest",
			ImageID: "sha256:image-c",
			Created: 1900000000,
			State:   "running",
			Status:  "Up 10 minutes",
			Labels: map[string]string{
				"com.getarcaneapp.internal.resource": "true",
			},
		},
	}
	images := []dockerimage.Summary{
		{ID: "sha256:image-a", RepoTags: []string{"repo/app:stable"}, Created: 1710000000, Size: 100},
		{ID: "sha256:image-b", RepoTags: []string{"repo/worker:latest"}, Created: 1720000000, Size: 250},
		{ID: "sha256:image-c", RepoTags: []string{"ghcr.io/getarcaneapp/arcane:latest"}, Created: 1730000000, Size: 175},
	}

	createDashboardTestImageUpdateRecord(t, db, models.ImageUpdateRecord{
		ID:         "sha256:image-b",
		Repository: "docker.io/repo/worker",
		Tag:        "latest",
		HasUpdate:  true,
	})

	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_snapshot",
		UserID:    new("user-1"),
		ExpiresAt: new(time.Now().Add(12 * time.Hour)),
	})

	volumes := []dockervolume.Volume{
		{Name: "app-data", Driver: "local"},
		{Name: "orphan-data", Driver: "local"},
	}

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images, volumes)
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)
	require.NoError(t, settingsSvc.SetStringSetting(context.Background(), "projectsDirectory", projectsDir))
	projectPath := createComposeProjectDirInternal(t, projectsDir, "project-with-update")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: repo/worker:latest\n"), 0o644))
	dirName := "project-with-update"
	require.NoError(t, db.WithContext(context.Background()).Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-with-update"},
		Name:      "project-with-update",
		DirName:   &dirName,
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}).Error)
	projectSvc := project.NewProjectService(db, settingsSvc, nil, image.NewImageService(db, nil, nil, nil, nil, nil), nil, nil, nil, nil, config.Load())
	svc := NewDashboardService(db, dockerSvc, nil, projectSvc, nil, settingsSvc, nil, nil, nil, volume.NewVolumeService(db, nil, nil, nil, nil, nil, nil, nil, nil, nil))

	snapshot, err := svc.GetSnapshot(context.Background(), DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	require.Len(t, snapshot.Containers.Data, 2)
	require.Equal(t, "container-stopped", snapshot.Containers.Data[0].ID)
	require.Equal(t, 1, snapshot.Containers.Counts.RunningContainers)
	require.Equal(t, 1, snapshot.Containers.Counts.StoppedContainers)
	require.Equal(t, 2, snapshot.Containers.Counts.TotalContainers)
	require.EqualValues(t, 2, snapshot.Containers.Pagination.TotalItems)

	require.Len(t, snapshot.Images.Data, 3)
	require.Equal(t, "sha256:image-b", snapshot.Images.Data[0].ID)
	require.Equal(t, 2, snapshot.ImageUsageCounts.Inuse)
	require.Equal(t, 1, snapshot.ImageUsageCounts.Unused)
	require.Equal(t, 3, snapshot.ImageUsageCounts.Total)
	require.EqualValues(t, 525, snapshot.ImageUsageCounts.TotalSize)
	require.Equal(t, dashboardtypes.SnapshotSettings{}, snapshot.Settings)

	require.NotNil(t, snapshot.VolumeUsageCounts)
	require.Equal(t, volumetypes.UsageCounts{Inuse: 1, Unused: 1, Total: 2}, *snapshot.VolumeUsageCounts)

	require.ElementsMatch(t, []dashboardtypes.ActionItem{
		{Kind: dashboardtypes.ActionItemKindStoppedContainers, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindImageUpdates, Count: 2, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindExpiringKeys, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
	}, snapshot.ActionItems.Items)
}

func TestDashboardService_GetSnapshot_DebugAllGoodOnlyClearsActionItems(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)

	containers := []dockercontainer.Summary{
		{
			ID:      "container-stopped",
			Names:   []string{"/stopped-app"},
			Image:   "repo/worker:latest",
			ImageID: "sha256:image-b",
			Created: 1800000000,
			State:   "exited",
			Status:  "Exited (0) 1 hour ago",
			Labels:  map[string]string{},
		},
	}
	images := []dockerimage.Summary{
		{ID: "sha256:image-b", RepoTags: []string{"repo/worker:latest"}, Created: 1720000000, Size: 250},
	}

	createDashboardTestImageUpdateRecord(t, db, models.ImageUpdateRecord{ID: "sha256:image-b", HasUpdate: true})

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images, nil)
	svc := NewDashboardService(db, dockerSvc, nil, nil, nil, settingsSvc, nil, nil, nil, nil)

	snapshot, err := svc.GetSnapshot(context.Background(), DashboardActionItemsOptions{DebugAllGood: true})
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	require.Len(t, snapshot.Containers.Data, 1)
	require.Len(t, snapshot.Images.Data, 1)
	require.Equal(t, 1, snapshot.Containers.Counts.StoppedContainers)
	require.Equal(t, 1, snapshot.ImageUsageCounts.Inuse)
	require.Nil(t, snapshot.VolumeUsageCounts)
	require.Empty(t, snapshot.ActionItems.Items)
}

// Test fixtures shared by this package's tests.

// createComposeProjectDirInternal writes a minimal single-service compose project under
// root and returns its path.
func createComposeProjectDirInternal(t *testing.T, root, name string) string {
	t.Helper()

	projectPath := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(projectPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: nginx:alpine\n"), 0o600))

	return projectPath
}

// createTestRemoteEnvironmentInternal inserts an enabled, online remote environment.
func createTestRemoteEnvironmentInternal(t *testing.T, db *database.DB, environmentID, name, apiURL, token string) {
	t.Helper()
	now := time.Now()
	require.NoError(t, db.Create(&models.Environment{
		BaseModel: models.BaseModel{
			ID:        environmentID,
			CreatedAt: now,
			UpdatedAt: &now,
		},
		Name:        name,
		ApiUrl:      apiURL,
		Status:      string(models.EnvironmentStatusOnline),
		Enabled:     true,
		AccessToken: &token,
	}).Error)
}

// newSettingsServiceForTestInternal builds a SettingsService backed by its own actor runtime,
// stopped when the test ends.
func newSettingsServiceForTestInternal(ctx context.Context, t testing.TB, db *database.DB) (*settings.SettingsService, error) {
	t.Helper()
	lifecycle := fxtest.NewLifecycle(t)
	runtime, err := actors.NewRuntime(ctx, lifecycle)
	require.NoError(t, err)
	executor, err := actors.NewExecutor(ctx, runtime, "settings-test", t.Name(), 3)
	require.NoError(t, err)
	effects, err := actors.NewExecutor(ctx, runtime, "settings-effects-test", t.Name(), 3)
	require.NoError(t, err)
	t.Cleanup(func() { //nolint:contextcheck // cleanup must outlive ctx to stop the runtime
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, executor.Stop(stopCtx))
		require.NoError(t, effects.Stop(stopCtx))
		require.NoError(t, lifecycle.Stop(stopCtx))
	})
	return settings.NewSettingsService(ctx, db, executor, effects)
}
