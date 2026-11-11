package dashboard

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/libtnb/sqlite"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"kit.getarcane.app/docker/streams/bus"

	"getarcane.dev/arcane/v3/internal/config"
	"getarcane.dev/arcane/v3/internal/container"
	"getarcane.dev/arcane/v3/internal/database"
	"getarcane.dev/arcane/v3/internal/docker"
	"getarcane.dev/arcane/v3/internal/settings"
	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

var (
	allSections = []string{
		types.DashboardSectionContainers,
		types.DashboardSectionProjects,
		types.DashboardSectionImages,
		types.DashboardSectionVulnerabilities,
		types.DashboardSectionRisk,
		types.DashboardSectionDiskUsage,
		types.DashboardSectionTopMemory,
		types.DashboardSectionVersion,
	}

	composeLabels = map[string]string{"com.docker.compose.project": "stack"}
)

func newTestDashboardService(t *testing.T, imagesStatus int) (*DashboardService, *settings.SettingsService) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			assert.NoError(t, json.MarshalWrite(w, []dockercontainer.Summary{
				{ID: "web", Names: []string{"/web"}, State: "running", Labels: composeLabels},
				{ID: "api", Names: []string{"/api"}, State: "running", Labels: composeLabels},
				{ID: "job", Names: []string{"/job"}, State: "exited", Labels: composeLabels},
			}))
		case strings.HasSuffix(r.URL.Path, "/images/json"):
			w.WriteHeader(imagesStatus)
			_, _ = w.Write([]byte(`{"message":"image store unavailable"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	dockerClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dockerClient.Close() })

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

	dockerService := docker.NewDockerClientService(t.Context(), db, settingsService, bus.Config{}).WithClient(dockerClient)
	return NewDashboardService(DashboardServiceParams{
		DockerService:    dockerService,
		ContainerService: container.NewContainerService(container.ContainerServiceParams{DockerService: dockerService}),
		SettingsService:  settingsService,
	}), settingsService
}

func environmentPermissions(perms ...string) *authz.PermissionSet {
	granted := make(map[string]struct{}, len(perms))
	for _, perm := range perms {
		granted[perm] = struct{}{}
	}
	return &authz.PermissionSet{
		Global: map[string]struct{}{},
		PerEnv: map[string]map[string]struct{}{"0": granted},
	}
}

func TestDashboardService_GetSummary_LoadsOnlyRequestedPermittedSections(t *testing.T) {
	service, _ := newTestDashboardService(t, http.StatusOK)

	summary := service.GetSummary(t.Context(), SummaryRequest{
		Permissions:   environmentPermissions(authz.PermContainersList, authz.PermImagesList),
		EnvironmentID: "0",
		Sections: []string{
			types.DashboardSectionContainers,
			types.DashboardSectionProjects,
			types.DashboardSectionVersion,
			"unknown",
		},
	})

	require.NotNil(t, summary.Containers)
	assert.Empty(t, summary.Containers.Error)
	assert.Equal(t, types.ContainerStatusCounts{
		RunningContainers: 2,
		StoppedContainers: 1,
		TotalContainers:   3,
	}, summary.Containers.Data)
	assert.Nil(t, summary.Projects)
	assert.Nil(t, summary.Version)
	assert.Nil(t, summary.Images)
}

func TestDashboardService_GetSummary_ReportsFailedSectionAlongsideLoadedOnes(t *testing.T) {
	service, _ := newTestDashboardService(t, http.StatusInternalServerError)

	summary := service.GetSummary(t.Context(), SummaryRequest{
		Permissions:   environmentPermissions(authz.PermContainersList, authz.PermImagesList),
		EnvironmentID: "0",
		Sections:      allSections,
	})

	require.NotNil(t, summary.Images)
	assert.Contains(t, summary.Images.Error, "image store unavailable")
	require.NotNil(t, summary.Containers)
	assert.Empty(t, summary.Containers.Error)
	assert.Equal(t, 3, summary.Containers.Data.TotalContainers)
}

func TestDashboardService_GetSummary_SkipsVulnerabilitySectionsWhenFeatureDisabled(t *testing.T) {
	service, settingsService := newTestDashboardService(t, http.StatusOK)
	require.NoError(t, settingsService.SetBoolSetting(t.Context(), types.FeatureVulnerabilityManagementSettingKey, false))

	summary := service.GetSummary(t.Context(), SummaryRequest{
		Permissions:   environmentPermissions(authz.PermVulnsList),
		EnvironmentID: "0",
		Sections:      allSections,
	})

	assert.Equal(t, types.DashboardSummary{}, summary)
}

func TestMergeSummaries_MergesRiskFromEnvironmentsThatLoadedIt(t *testing.T) {
	merged := mergeSummaries([]types.EnvironmentData[types.DashboardSummary]{
		{
			Environment: types.EnvironmentRef{ID: "0", Name: "local"},
			Data: types.DashboardSummary{
				Risk: &types.DashboardSection[*types.RiskOverview]{
					Data: &types.RiskOverview{Summary: types.VulnerabilitySeveritySummary{Critical: 2, Total: 2}},
				},
			},
		},
		{
			Environment: types.EnvironmentRef{ID: "1", Name: "edge"},
			Data: types.DashboardSummary{
				Risk: &types.DashboardSection[*types.RiskOverview]{
					Data: &types.RiskOverview{Summary: types.VulnerabilitySeveritySummary{High: 3, Total: 3}},
				},
			},
		},
		{
			Environment: types.EnvironmentRef{ID: "2", Name: "offline"},
			Data: types.DashboardSummary{
				Risk: &types.DashboardSection[*types.RiskOverview]{Error: "scanner unavailable"},
			},
		},
	})

	require.NotNil(t, merged.Risk)
	require.NotNil(t, merged.Risk.Data)
	assert.Empty(t, merged.Risk.Error)
	assert.Equal(t, types.VulnerabilitySeveritySummary{Critical: 2, High: 3, Total: 5}, merged.Risk.Data.Summary)
	assert.Nil(t, merged.Containers)
}

func TestMergeSummaries_OmitsRiskWhenNoEnvironmentLoadedIt(t *testing.T) {
	merged := mergeSummaries([]types.EnvironmentData[types.DashboardSummary]{
		{
			Environment: types.EnvironmentRef{ID: "0", Name: "local"},
			Data: types.DashboardSummary{
				Containers: &types.DashboardSection[types.ContainerStatusCounts]{
					Data: types.ContainerStatusCounts{TotalContainers: 4},
				},
			},
		},
	})

	assert.Equal(t, types.DashboardSummary{}, merged)
}
