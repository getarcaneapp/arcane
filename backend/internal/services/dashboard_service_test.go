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
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/types/base"
	containertypes "github.com/getarcaneapp/arcane/types/container"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	imagetypes "github.com/getarcaneapp/arcane/types/image"
	versiontypes "github.com/getarcaneapp/arcane/types/version"
	glsqlite "github.com/glebarez/sqlite"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardServiceTestDB(t *testing.T) (*database.DB, *SettingsService) {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ApiKey{}, &models.Environment{}, &models.ImageUpdateRecord{}, &models.Project{}, &models.SettingVariable{}))

	databaseDB := &database.DB{DB: db}
	settingsSvc, err := NewSettingsService(context.Background(), databaseDB)
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

func createDashboardTestEnvironment(t *testing.T, db *database.DB, env models.Environment) {
	t.Helper()
	require.NoError(t, db.WithContext(context.Background()).Create(&env).Error)
}

func newDashboardTestDockerService(
	t *testing.T,
	settingsSvc *SettingsService,
	containers []dockercontainer.Summary,
	images []dockerimage.Summary,
) *DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode(containers))
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			require.NoError(t, json.NewEncoder(w).Encode(images))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.41"),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dockerClient.Close()
	})

	return &DockerClientService{
		client:          dockerClient,
		settingsService: settingsSvc,
	}
}

func newDashboardTestVersionServiceInternal() *VersionService {
	return NewVersionService(nil, true, "1.2.3", "abcdef1234567890", nil, nil)
}

func TestDashboardService_GetActionItems_IncludesExpiringAPIKeys(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)
	svc := NewDashboardService(db, nil, nil, nil, settingsSvc, nil, nil, nil)

	now := time.Now()
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_s",
		UserID:    "user-1",
		ExpiresAt: new(now.Add(24 * time.Hour)),
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "already-expired",
		KeyHash:   "hash-expired",
		KeyPrefix: "arc_test_e",
		UserID:    "user-1",
		ExpiresAt: new(now.Add(-24 * time.Hour)),
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "future",
		KeyHash:   "hash-future",
		KeyPrefix: "arc_test_f",
		UserID:    "user-1",
		ExpiresAt: new(now.Add(45 * 24 * time.Hour)),
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "never-expires",
		KeyHash:   "hash-never",
		KeyPrefix: "arc_test_n",
		UserID:    "user-1",
	})

	actionItems, err := svc.GetActionItems(context.Background(), DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, actionItems)
	require.Len(t, actionItems.Items, 1)

	item := actionItems.Items[0]
	require.Equal(t, dashboardtypes.ActionItemKindExpiringKeys, item.Kind)
	require.Equal(t, 2, item.Count)
	require.Equal(t, dashboardtypes.ActionItemSeverityWarning, item.Severity)
}

func TestDashboardService_GetActionItems_DebugAllGoodReturnsNoItems(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)
	svc := NewDashboardService(db, nil, nil, nil, settingsSvc, nil, nil, nil)

	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_d",
		UserID:    "user-1",
		ExpiresAt: new(time.Now().Add(2 * time.Hour)),
	})

	actionItems, err := svc.GetActionItems(context.Background(), DashboardActionItemsOptions{
		DebugAllGood: true,
	})
	require.NoError(t, err)
	require.NotNil(t, actionItems)
	require.Empty(t, actionItems.Items)
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
		UserID:    "user-1",
		ExpiresAt: new(time.Now().Add(12 * time.Hour)),
	})

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images)
	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)
	require.NoError(t, settingsSvc.SetStringSetting(context.Background(), "projectsDirectory", projectsDir))
	projectPath := createComposeProjectDir(t, projectsDir, "project-with-update")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: repo/worker:latest\n"), 0o644))
	require.NoError(t, db.WithContext(context.Background()).Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-with-update"},
		Name:      "project-with-update",
		DirName:   ptr("project-with-update"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}).Error)
	projectSvc := NewProjectService(db, settingsSvc, nil, &ImageService{db: db}, nil, nil, config.Load())
	svc := NewDashboardService(db, dockerSvc, nil, projectSvc, settingsSvc, nil, nil, nil)

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

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images)
	svc := NewDashboardService(db, dockerSvc, nil, nil, settingsSvc, nil, nil, nil)

	snapshot, err := svc.GetSnapshot(context.Background(), DashboardActionItemsOptions{DebugAllGood: true})
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	require.Len(t, snapshot.Containers.Data, 1)
	require.Len(t, snapshot.Images.Data, 1)
	require.Equal(t, 1, snapshot.Containers.Counts.StoppedContainers)
	require.Equal(t, 1, snapshot.ImageUsageCounts.Inuse)
	require.Empty(t, snapshot.ActionItems.Items)
}

func TestDashboardService_GetEnvironmentsOverview_ReturnsLocalAndRemoteSummaries(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)

	containers := []dockercontainer.Summary{
		{
			ID:      "local-running",
			Names:   []string{"/local-running"},
			Image:   "repo/local:stable",
			ImageID: "sha256:local-image",
			Created: 1700000000,
			State:   "running",
			Status:  "Up 1 hour",
			Labels:  map[string]string{},
		},
	}
	images := []dockerimage.Summary{
		{ID: "sha256:local-image", RepoTags: []string{"repo/local:stable"}, Created: 1710000000, Size: 150},
	}

	remoteSnapshot := base.ApiResponse[dashboardtypes.Snapshot]{
		Success: true,
		Data: dashboardtypes.Snapshot{
			Containers: dashboardtypes.SnapshotContainers{
				Counts: containertypes.StatusCounts{
					RunningContainers: 2,
					StoppedContainers: 1,
					TotalContainers:   3,
				},
			},
			ImageUsageCounts: imagetypes.UsageCounts{
				Inuse:     2,
				Unused:    3,
				Total:     5,
				TotalSize: 900,
			},
			ActionItems: dashboardtypes.ActionItems{
				Items: []dashboardtypes.ActionItem{
					{Kind: dashboardtypes.ActionItemKindStoppedContainers, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
				},
			},
		},
	}

	remoteVersion := versiontypes.Info{
		CurrentVersion:  "v2.4.0",
		DisplayVersion:  "v2.4.0",
		Revision:        "1234567890abcdef",
		ShortRevision:   "12345678",
		GoVersion:       "go1.24.0",
		IsSemverVersion: true,
		UpdateAvailable: true,
		NewestVersion:   "v2.5.0",
		ReleaseURL:      "https://github.com/getarcaneapp/arcane/releases/tag/v2.5.0",
	}

	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/environments/0/dashboard":
			require.NoError(t, json.NewEncoder(w).Encode(remoteSnapshot))
		case "/api/app-version":
			require.NoError(t, json.NewEncoder(w).Encode(remoteVersion))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remoteServer.Close)

	createDashboardTestEnvironment(t, db, models.Environment{
		BaseModel: models.BaseModel{ID: "0", CreatedAt: time.Now()},
		Name:      "Local Docker",
		ApiUrl:    "http://local.test",
		Status:    string(models.EnvironmentStatusOnline),
		Enabled:   true,
	})
	createDashboardTestEnvironment(t, db, models.Environment{
		BaseModel: models.BaseModel{ID: "env-remote", CreatedAt: time.Now()},
		Name:      "Remote Alpha",
		ApiUrl:    remoteServer.URL,
		Status:    string(models.EnvironmentStatusOnline),
		Enabled:   true,
	})

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images)
	envSvc := NewEnvironmentService(db, remoteServer.Client(), nil, nil, settingsSvc, nil)
	svc := NewDashboardService(db, dockerSvc, nil, nil, settingsSvc, nil, envSvc, newDashboardTestVersionServiceInternal())

	overview, err := svc.GetEnvironmentsOverview(context.Background(), DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, overview)
	require.Len(t, overview.Environments, 2)

	require.Equal(t, 2, overview.Summary.TotalEnvironments)
	require.Equal(t, 2, overview.Summary.OnlineEnvironments)
	require.Equal(t, 4, overview.Summary.Containers.TotalContainers)
	require.Equal(t, 6, overview.Summary.ImageUsageCounts.Total)
	require.Equal(t, 1, overview.Summary.EnvironmentsWithActionItems)

	require.Equal(t, "0", overview.Environments[0].Environment.ID)
	require.Equal(t, dashboardtypes.EnvironmentSnapshotStateReady, overview.Environments[0].SnapshotState)
	require.Equal(t, 1, overview.Environments[0].Containers.TotalContainers)
	require.NotNil(t, overview.Environments[0].VersionInfo)
	require.Equal(t, "v1.2.3", overview.Environments[0].VersionInfo.CurrentVersion)

	require.Equal(t, "env-remote", overview.Environments[1].Environment.ID)
	require.Equal(t, dashboardtypes.EnvironmentSnapshotStateReady, overview.Environments[1].SnapshotState)
	require.Equal(t, 3, overview.Environments[1].Containers.TotalContainers)
	require.Len(t, overview.Environments[1].ActionItems.Items, 1)
	require.NotNil(t, overview.Environments[1].VersionInfo)
	require.Equal(t, "v2.5.0", overview.Environments[1].VersionInfo.NewestVersion)
}

func TestDashboardService_GetEnvironmentsOverview_HandlesRemoteSnapshotFailure(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)

	createDashboardTestEnvironment(t, db, models.Environment{
		BaseModel: models.BaseModel{ID: "env-offline", CreatedAt: time.Now()},
		Name:      "Offline Env",
		ApiUrl:    "http://offline.test",
		Status:    string(models.EnvironmentStatusOffline),
		Enabled:   true,
	})
	createDashboardTestEnvironment(t, db, models.Environment{
		BaseModel: models.BaseModel{ID: "env-error", CreatedAt: time.Now()},
		Name:      "Broken Env",
		ApiUrl:    "http://127.0.0.1:1",
		Status:    string(models.EnvironmentStatusOnline),
		Enabled:   true,
	})

	envSvc := NewEnvironmentService(db, http.DefaultClient, nil, nil, settingsSvc, nil)
	svc := NewDashboardService(db, nil, nil, nil, settingsSvc, nil, envSvc, newDashboardTestVersionServiceInternal())

	overview, err := svc.GetEnvironmentsOverview(context.Background(), DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, overview)
	require.Len(t, overview.Environments, 2)

	byID := make(map[string]dashboardtypes.EnvironmentOverview, len(overview.Environments))
	for _, item := range overview.Environments {
		byID[item.Environment.ID] = item
	}

	require.Equal(t, dashboardtypes.EnvironmentSnapshotStateSkipped, byID["env-offline"].SnapshotState)
	require.Nil(t, byID["env-offline"].SnapshotError)
	require.Nil(t, byID["env-offline"].VersionInfo)

	require.Equal(t, dashboardtypes.EnvironmentSnapshotStateError, byID["env-error"].SnapshotState)
	require.NotNil(t, byID["env-error"].SnapshotError)
	require.Contains(t, *byID["env-error"].SnapshotError, "failed to proxy dashboard snapshot")
	require.Nil(t, byID["env-error"].VersionInfo)
}

func TestDashboardService_GetEnvironmentsOverview_OmitsVersionInfoWhenFetchFails(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)

	remoteSnapshot := base.ApiResponse[dashboardtypes.Snapshot]{
		Success: true,
		Data: dashboardtypes.Snapshot{
			Containers: dashboardtypes.SnapshotContainers{
				Counts: containertypes.StatusCounts{
					RunningContainers: 1,
					StoppedContainers: 0,
					TotalContainers:   1,
				},
			},
			ImageUsageCounts: imagetypes.UsageCounts{
				Inuse:     1,
				Unused:    0,
				Total:     1,
				TotalSize: 128,
			},
			ActionItems: dashboardtypes.ActionItems{Items: []dashboardtypes.ActionItem{}},
		},
	}

	remoteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/environments/0/dashboard":
			require.NoError(t, json.NewEncoder(w).Encode(remoteSnapshot))
		case "/api/app-version":
			http.Error(w, "version unavailable", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(remoteServer.Close)

	createDashboardTestEnvironment(t, db, models.Environment{
		BaseModel: models.BaseModel{ID: "env-remote", CreatedAt: time.Now()},
		Name:      "Remote Alpha",
		ApiUrl:    remoteServer.URL,
		Status:    string(models.EnvironmentStatusOnline),
		Enabled:   true,
	})

	envSvc := NewEnvironmentService(db, remoteServer.Client(), nil, nil, settingsSvc, nil)
	svc := NewDashboardService(db, nil, nil, nil, settingsSvc, nil, envSvc, newDashboardTestVersionServiceInternal())

	overview, err := svc.GetEnvironmentsOverview(context.Background(), DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, overview)
	require.Len(t, overview.Environments, 1)

	require.Equal(t, dashboardtypes.EnvironmentSnapshotStateReady, overview.Environments[0].SnapshotState)
	require.Equal(t, 1, overview.Environments[0].Containers.TotalContainers)
	require.Nil(t, overview.Environments[0].VersionInfo)
}

func TestDashboardService_GetActionItems_CountsAffectedResources(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)
	ctx := context.Background()

	containers := []dockercontainer.Summary{
		{
			ID:      "container-updated",
			Names:   []string{"/updated-app"},
			Image:   "repo/app:latest",
			ImageID: "sha256:image-a",
			Created: 1700000000,
			State:   "running",
			Status:  "Up 2 hours",
			Labels:  map[string]string{},
		},
	}
	images := []dockerimage.Summary{
		{ID: "sha256:image-a", RepoTags: []string{"repo/app:latest"}, Created: 1710000000, Size: 100},
		{ID: "sha256:image-unused", RepoTags: []string{"repo/unused:latest"}, Created: 1710000001, Size: 50},
	}

	createDashboardTestImageUpdateRecord(t, db, models.ImageUpdateRecord{
		ID:         "sha256:image-a",
		Repository: "docker.io/repo/app",
		Tag:        "latest",
		HasUpdate:  true,
	})
	createDashboardTestImageUpdateRecord(t, db, models.ImageUpdateRecord{
		ID:         "sha256:image-unused",
		Repository: "docker.io/repo/unused",
		Tag:        "latest",
		HasUpdate:  true,
	})

	projectsDir := t.TempDir()
	t.Setenv("PROJECTS_DIRECTORY", projectsDir)
	require.NoError(t, settingsSvc.SetStringSetting(ctx, "projectsDirectory", projectsDir))
	projectPath := createComposeProjectDir(t, projectsDir, "project-with-update")
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "compose.yaml"), []byte("services:\n  app:\n    image: repo/app:latest\n"), 0o644))
	require.NoError(t, db.WithContext(ctx).Create(&models.Project{
		BaseModel: models.BaseModel{ID: "project-with-update"},
		Name:      "project-with-update",
		DirName:   ptr("project-with-update"),
		Path:      projectPath,
		Status:    models.ProjectStatusStopped,
	}).Error)

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images)
	projectSvc := NewProjectService(db, settingsSvc, nil, &ImageService{db: db}, nil, nil, config.Load())
	svc := NewDashboardService(db, dockerSvc, nil, projectSvc, settingsSvc, nil, nil, nil)

	actionItems, err := svc.GetActionItems(ctx, DashboardActionItemsOptions{})
	require.NoError(t, err)
	require.NotNil(t, actionItems)

	require.Len(t, actionItems.Items, 1)
	assert.Equal(t, dashboardtypes.ActionItemKindImageUpdates, actionItems.Items[0].Kind)
	assert.Equal(t, 2, actionItems.Items[0].Count)
}
