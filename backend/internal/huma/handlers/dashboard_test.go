package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/services"
	dashboardtypes "github.com/getarcaneapp/arcane/types/dashboard"
	glsqlite "github.com/glebarez/sqlite"
	dockercontainer "github.com/moby/moby/api/types/container"
	dockerimage "github.com/moby/moby/api/types/image"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardHandlerTestDB(t *testing.T) (*database.DB, *services.SettingsService) {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ApiKey{}, &models.Environment{}, &models.ImageUpdateRecord{}, &models.Project{}, &models.SettingVariable{}))

	databaseDB := &database.DB{DB: db}
	settingsSvc, err := services.NewSettingsService(context.Background(), databaseDB)
	require.NoError(t, err)

	return databaseDB, settingsSvc
}

func newDashboardHandlerTestDockerService(
	t *testing.T,
	settingsSvc *services.SettingsService,
	containers []dockercontainer.Summary,
	images []dockerimage.Summary,
) *services.DockerClientService {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			w.Header().Set("API-Version", "1.41")
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			require.NoError(t, json.NewEncoder(w).Encode(containers))
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			require.NoError(t, json.NewEncoder(w).Encode(images))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return services.NewDockerClientService(
		nil,
		&config.Config{DockerHost: server.URL},
		settingsSvc,
	)
}

func TestDashboardHandlerGetDashboardReturnsSnapshot(t *testing.T) {
	db, settingsSvc := setupDashboardHandlerTestDB(t)

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
	}
	images := []dockerimage.Summary{
		{ID: "sha256:image-a", RepoTags: []string{"repo/app:stable"}, Created: 1710000000, Size: 100},
		{ID: "sha256:image-b", RepoTags: []string{"repo/worker:latest"}, Created: 1720000000, Size: 250},
	}

	require.NoError(t, db.WithContext(context.Background()).Create(&models.ImageUpdateRecord{
		ID:        "sha256:image-b",
		HasUpdate: true,
	}).Error)
	require.NoError(t, db.WithContext(context.Background()).Create(&models.ApiKey{
		Name:      "expiring-soon",
		KeyHash:   "hash-soon",
		KeyPrefix: "arc_test_handler",
		UserID:    new("user-1"),
		ExpiresAt: new(time.Now().Add(12 * time.Hour)),
	}).Error)

	dockerSvc := newDashboardHandlerTestDockerService(t, settingsSvc, containers, images)
	handler := &DashboardHandler{
		dashboardService: services.NewDashboardService(db, dockerSvc, nil, nil, nil, settingsSvc, nil, nil, nil),
	}

	output, err := handler.GetDashboard(context.Background(), &GetDashboardInput{EnvironmentID: "0"})
	require.NoError(t, err)
	require.NotNil(t, output)
	require.True(t, output.Body.Success)

	snapshot := output.Body.Data
	require.Len(t, snapshot.Containers.Data, 2)
	require.Len(t, snapshot.Images.Data, 2)
	require.Equal(t, 1, snapshot.Containers.Counts.RunningContainers)
	require.Equal(t, 1, snapshot.Containers.Counts.StoppedContainers)
	require.Equal(t, dashboardtypes.SnapshotSettings{}, snapshot.Settings)
	require.ElementsMatch(t, []dashboardtypes.ActionItem{
		{Kind: dashboardtypes.ActionItemKindStoppedContainers, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindImageUpdates, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
		{Kind: dashboardtypes.ActionItemKindExpiringKeys, Count: 1, Severity: dashboardtypes.ActionItemSeverityWarning},
	}, snapshot.ActionItems.Items)
}

func TestDashboardHandlerGetEnvironmentsOverviewReturnsAggregateSummary(t *testing.T) {
	db, settingsSvc := setupDashboardHandlerTestDB(t)

	require.NoError(t, db.WithContext(context.Background()).Create(&models.Environment{
		BaseModel: models.BaseModel{ID: "0", CreatedAt: time.Now()},
		Name:      "Local Docker",
		ApiUrl:    "http://local.test",
		Status:    string(models.EnvironmentStatusOffline),
		Enabled:   true,
	}).Error)

	handler := &DashboardHandler{
		dashboardService: services.NewDashboardService(
			db,
			nil,
			nil,
			nil,
			nil,
			settingsSvc,
			nil,
			services.NewEnvironmentService(db, http.DefaultClient, nil, nil, settingsSvc, nil),
			services.NewVersionService(nil, true, "1.2.3", "abcdef1234567890", nil, nil),
		),
	}

	output, err := handler.GetEnvironmentsOverview(context.Background(), &GetDashboardEnvironmentsOverviewInput{})
	require.NoError(t, err)
	require.NotNil(t, output)
	require.True(t, output.Body.Success)
	require.Equal(t, 1, output.Body.Data.Summary.TotalEnvironments)
	require.Len(t, output.Body.Data.Environments, 1)
	require.Equal(t, "0", output.Body.Data.Environments[0].Environment.ID)
	require.Equal(t, dashboardtypes.EnvironmentSnapshotStateSkipped, output.Body.Data.Environments[0].SnapshotState)
	require.Nil(t, output.Body.Data.Environments[0].VersionInfo)
}
