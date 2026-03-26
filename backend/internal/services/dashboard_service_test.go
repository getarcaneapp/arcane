package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	glsqlite "github.com/glebarez/sqlite"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardServiceTestDB(t *testing.T) (*database.DB, *SettingsService) {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ApiKey{}, &models.ImageUpdateRecord{}, &models.Project{}, &models.SettingVariable{}))

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

func TestDashboardService_GetActionItems_IncludesExpiringAPIKeys(t *testing.T) {
	db, settingsSvc := setupDashboardServiceTestDB(t)
	svc := NewDashboardService(db, nil, nil, settingsSvc, nil)

	now := time.Now()
	expiringSoon := now.Add(24 * time.Hour)
	alreadyExpired := now.Add(-24 * time.Hour)
	farFuture := now.Add(45 * 24 * time.Hour)

	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_s",
		UserID:    "user-1",
		ExpiresAt: &expiringSoon,
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "already-expired",
		KeyHash:   "hash-expired",
		KeyPrefix: "arc_test_e",
		UserID:    "user-1",
		ExpiresAt: &alreadyExpired,
	})
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "future",
		KeyHash:   "hash-future",
		KeyPrefix: "arc_test_f",
		UserID:    "user-1",
		ExpiresAt: &farFuture,
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
	svc := NewDashboardService(db, nil, nil, settingsSvc, nil)

	expiresAt := time.Now().Add(2 * time.Hour)
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_d",
		UserID:    "user-1",
		ExpiresAt: &expiresAt,
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

	createDashboardTestImageUpdateRecord(t, db, models.ImageUpdateRecord{ID: "sha256:image-b", HasUpdate: true})

	expiresSoon := time.Now().Add(12 * time.Hour)
	createDashboardTestAPIKey(t, db, models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_snapshot",
		UserID:    "user-1",
		ExpiresAt: &expiresSoon,
	})

	dockerSvc := newDashboardTestDockerService(t, settingsSvc, containers, images)
	svc := NewDashboardService(db, dockerSvc, nil, settingsSvc, nil)

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
	require.Equal(t, "dangling", snapshot.Settings.DockerPruneMode)

	require.ElementsMatch(t, []dashboardtypes.ActionItem{
		{Kind: dashboardtypes.ActionItemKindStoppedContainers, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindImageUpdates, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
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
	svc := NewDashboardService(db, dockerSvc, nil, settingsSvc, nil)

	snapshot, err := svc.GetSnapshot(context.Background(), DashboardActionItemsOptions{DebugAllGood: true})
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	require.Len(t, snapshot.Containers.Data, 1)
	require.Len(t, snapshot.Images.Data, 1)
	require.Equal(t, 1, snapshot.Containers.Counts.StoppedContainers)
	require.Equal(t, 1, snapshot.ImageUsageCounts.Inuse)
	require.Empty(t, snapshot.ActionItems.Items)
}
