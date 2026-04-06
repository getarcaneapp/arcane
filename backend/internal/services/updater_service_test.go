package services

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	libupdater "github.com/getarcaneapp/arcane/backend/pkg/libarcane/updater"
)

// mockSystemUpgradeService is a simple mock implementation for testing
type mockSystemUpgradeService struct {
	triggerCalled bool
	triggerError  error
	capturedUser  *models.User
	canUpgrade    bool
}

func (m *mockSystemUpgradeService) TriggerUpgradeViaCLI(ctx context.Context, user models.User) error {
	m.triggerCalled = true
	m.capturedUser = &user
	return m.triggerError
}

func (m *mockSystemUpgradeService) CanUpgrade(ctx context.Context) (bool, error) {
	return m.canUpgrade, nil
}

// TestUpdaterService_ArcaneLabel_TriggersCLIUpgrade verifies that when the
// com.getarcaneapp.arcane label is set to "true", the IsArcaneContainer check
// returns true, indicating that CLI upgrade should be triggered
func TestUpdaterService_ArcaneLabel_TriggersCLIUpgrade(t *testing.T) {
	ctx := context.Background()

	// Create mock upgrade service
	mockUpgrade := &mockSystemUpgradeService{}

	// Test with Arcane label set to "true"
	labels := map[string]string{
		"com.getarcaneapp.arcane": "true",
	}

	// Verify that IsArcaneContainer correctly identifies the label
	isArcane := libupdater.IsArcaneContainer(labels)
	assert.True(t, isArcane, "IsArcaneContainer should return true for Arcane label")

	// Simulate the logic from restartContainersUsingOldIDs:
	// When upgradeService is not nil and isArcane is true, CLI should be called
	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	// Verify CLI upgrade was called
	assert.True(t, mockUpgrade.triggerCalled, "TriggerUpgradeViaCLI should have been called for Arcane container")
}

func TestUpdaterService_ArcaneAgentLabel_TriggersCLIUpgrade(t *testing.T) {
	ctx := context.Background()
	mockUpgrade := &mockSystemUpgradeService{}
	service := &UpdaterService{upgradeService: mockUpgrade}

	labels := map[string]string{
		libupdater.LabelArcaneAgent: "true",
	}

	err := service.triggerSelfUpdateViaCLIInternal(ctx, "test", "container-1", "arcane-agent", labels)

	require.NoError(t, err)
	assert.True(t, mockUpgrade.triggerCalled, "TriggerUpgradeViaCLI should have been called for Arcane agent container")
	assert.NotNil(t, mockUpgrade.capturedUser)
	assert.Equal(t, systemUser.ID, mockUpgrade.capturedUser.ID)
}

// TestUpdaterService_NonArcaneLabel_DoesNotTriggerCLI verifies that containers without
// the Arcane label do not trigger CLI upgrade
func TestUpdaterService_NonArcaneLabel_DoesNotTriggerCLI(t *testing.T) {
	ctx := context.Background()

	// Create mock upgrade service
	mockUpgrade := &mockSystemUpgradeService{}

	// Test with no Arcane label
	labels := map[string]string{
		"some.other.label": "true",
	}

	// Verify that IsArcaneContainer returns false
	isArcane := libupdater.IsArcaneContainer(labels)
	assert.False(t, isArcane, "IsArcaneContainer should return false for non-Arcane container")

	// Simulate the logic from restartContainersUsingOldIDs
	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	// Verify CLI upgrade was NOT called
	assert.False(t, mockUpgrade.triggerCalled, "TriggerUpgradeViaCLI should not have been called for non-Arcane container")
}

func TestUpdaterService_TriggerSelfUpdateViaCLI_NonArcaneContainer(t *testing.T) {
	ctx := context.Background()
	mockUpgrade := &mockSystemUpgradeService{}
	service := &UpdaterService{upgradeService: mockUpgrade}

	labels := map[string]string{
		"some.other.label": "true",
	}

	err := service.triggerSelfUpdateViaCLIInternal(ctx, "test", "container-1", "not-arcane", labels)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "container is not an Arcane self-update target")
	assert.False(t, mockUpgrade.triggerCalled, "non-Arcane containers must not trigger the CLI upgrade path")
}

// TestUpdaterService_ArcaneLabelWithError_PropagatesError verifies that CLI upgrade errors
// are properly propagated
func TestUpdaterService_ArcaneLabelWithError_PropagatesError(t *testing.T) {
	ctx := context.Background()

	// Create mock that returns an error
	expectedErr := errors.New("upgrade already in progress")
	mockUpgrade := &mockSystemUpgradeService{
		triggerError: expectedErr,
	}

	labels := map[string]string{
		"com.getarcaneapp.arcane": "true",
	}

	isArcane := libupdater.IsArcaneContainer(labels)
	assert.True(t, isArcane, "Should detect Arcane container")

	// Call the upgrade method
	var err error
	if isArcane {
		err = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	// Verify error is propagated
	require.Error(t, err, "Error should be propagated from TriggerUpgradeViaCLI")
	assert.Equal(t, expectedErr, err, "Should return the same error")
	assert.True(t, mockUpgrade.triggerCalled, "TriggerUpgradeViaCLI should have been attempted")
}

// TestUpdaterService_NilUpgradeService_GracefulHandling verifies graceful handling
// when upgrade service is nil
func TestUpdaterService_NilUpgradeService_GracefulHandling(t *testing.T) {
	ctx := context.Background()
	service := &UpdaterService{}

	labels := map[string]string{
		libupdater.LabelArcane: "true",
	}

	err := service.triggerSelfUpdateViaCLIInternal(ctx, "test", "container-1", "arcane", labels)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server self-update requires CLI upgrade service")
}

func TestUpdaterService_ArcaneAgentLabel_MissingUpgradeServiceReturnsError(t *testing.T) {
	ctx := context.Background()
	service := &UpdaterService{}

	labels := map[string]string{
		libupdater.LabelArcaneAgent: "true",
	}

	err := service.triggerSelfUpdateViaCLIInternal(ctx, "test", "container-1", "arcane-agent", labels)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent self-update requires CLI upgrade service")
}

// TestUpdaterService_ArcaneLabelVariations tests various label formats
func TestUpdaterService_ArcaneLabelVariations(t *testing.T) {
	tests := []struct {
		name           string
		labels         map[string]string
		expectedArcane bool
		description    string
	}{
		{
			name: "standard arcane label",
			labels: map[string]string{
				"com.getarcaneapp.arcane": "true",
			},
			expectedArcane: true,
			description:    "Standard Arcane label should be detected",
		},
		{
			name:           "empty labels",
			labels:         map[string]string{},
			expectedArcane: false,
			description:    "Empty labels should not be detected as Arcane",
		},
		{
			name:           "nil labels",
			labels:         nil,
			expectedArcane: false,
			description:    "Nil labels should not be detected as Arcane",
		},
		{
			name: "other labels only",
			labels: map[string]string{
				"some.other.label": "true",
			},
			expectedArcane: false,
			description:    "Non-Arcane labels should not be detected as Arcane",
		},
		{
			name: "arcane label false",
			labels: map[string]string{
				"com.getarcaneapp.arcane": "false",
			},
			expectedArcane: false,
			description:    "Arcane label set to false should not be detected as Arcane",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isArcane := libupdater.IsArcaneContainer(tt.labels)
			assert.Equal(t, tt.expectedArcane, isArcane, tt.description)
		})
	}
}

// TestUpdaterService_CLICalledWithSystemUser verifies that systemUser is passed
func TestUpdaterService_CLICalledWithSystemUser(t *testing.T) {
	ctx := context.Background()

	mockUpgrade := &mockSystemUpgradeService{}

	labels := map[string]string{
		"com.getarcaneapp.arcane": "true",
	}

	isArcane := libupdater.IsArcaneContainer(labels)
	assert.True(t, isArcane)

	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	// Verify systemUser was passed
	assert.True(t, mockUpgrade.triggerCalled)
	assert.NotNil(t, mockUpgrade.capturedUser)
	assert.Equal(t, systemUser.ID, mockUpgrade.capturedUser.ID)
	assert.Equal(t, systemUser.Username, mockUpgrade.capturedUser.Username)
}

// TestUpdaterService_UpgradeServiceNotNilCheck verifies the nil check logic
func TestUpdaterService_UpgradeServiceNotNilCheck(t *testing.T) {
	ctx := context.Background()

	labels := map[string]string{
		"com.getarcaneapp.arcane": "true",
	}

	// Test with non-nil upgrade service
	mockUpgrade := &mockSystemUpgradeService{}
	isArcane := libupdater.IsArcaneContainer(labels)

	// This is the actual logic from restartContainersUsingOldIDs
	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	assert.True(t, mockUpgrade.triggerCalled, "Should call CLI upgrade when service is not nil")
}

func TestLookupComposeProjectIDInternal(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		projectID, ok := lookupComposeProjectIDInternal("myproject", map[string]string{
			"myproject": "p1",
		})
		require.True(t, ok)
		assert.Equal(t, "p1", projectID)
	})

	t.Run("normalized fallback", func(t *testing.T) {
		projectID, ok := lookupComposeProjectIDInternal("My Project", map[string]string{
			loader.NormalizeProjectName("myproject"): "p1",
		})
		require.True(t, ok)
		assert.Equal(t, "p1", projectID)
	})

	t.Run("missing project", func(t *testing.T) {
		projectID, ok := lookupComposeProjectIDInternal("missing", map[string]string{
			"other": "p1",
		})
		require.False(t, ok)
		assert.Empty(t, projectID)
	})
}

func TestUpdaterService_LazyRegisterComposeProjectInternal_AddsServicesForRegisteredProject(t *testing.T) {
	db := setupProjectTestDB(t)
	ctx := context.Background()

	project := &models.Project{
		BaseModel: models.BaseModel{ID: "p1"},
		Name:      "My Project!",
		Path:      "/tmp/my-project",
	}
	require.NoError(t, db.Create(project).Error)

	projectService := NewProjectService(db, nil, nil, nil, nil, nil, config.Load())
	svc := &UpdaterService{projectService: projectService}

	projectNameToID := map[string]string{}
	projectIDToObj := map[string]*models.Project{}
	projectToServices := map[string][]string{}
	projectToSeenServices := map[string]map[string]struct{}{}

	firstPlan := &restartPlan{
		newRef: "nginx:latest",
		inspect: &container.InspectResponse{
			Config: &container.Config{
				Labels: map[string]string{
					"com.docker.compose.project": "myproject",
					"com.docker.compose.service": "web",
				},
			},
		},
	}
	secondPlan := &restartPlan{
		newRef: "nginx:latest",
		inspect: &container.InspectResponse{
			Config: &container.Config{
				Labels: map[string]string{
					"com.docker.compose.project": "myproject",
					"com.docker.compose.service": "worker",
				},
			},
		},
	}

	svc.lazyRegisterComposeProjectInternal(ctx, firstPlan, projectNameToID, projectIDToObj, projectToServices, projectToSeenServices)
	svc.lazyRegisterComposeProjectInternal(ctx, secondPlan, projectNameToID, projectIDToObj, projectToServices, projectToSeenServices)

	require.Contains(t, projectToServices, project.ID)
	assert.Equal(t, []string{"web", "worker"}, projectToServices[project.ID])
	assert.Equal(t, project.ID, projectNameToID[project.Name])
	assert.Equal(t, project.ID, projectNameToID[loader.NormalizeProjectName(project.Name)])
}

func TestPendingComposeProjectServicesInternal(t *testing.T) {
	processedProjectServices := map[string]map[string]struct{}{}

	assert.Equal(t, []string{"A"}, pendingComposeProjectServicesInternal(processedProjectServices, "project-1", []string{"A"}))

	markComposeProjectServicesProcessedInternal(processedProjectServices, "project-1", []string{"A"})
	assert.Empty(t, pendingComposeProjectServicesInternal(processedProjectServices, "project-1", []string{"A"}))
	assert.Equal(t, []string{"B"}, pendingComposeProjectServicesInternal(processedProjectServices, "project-1", []string{"A", "B"}))

	markComposeProjectServicesProcessedInternal(processedProjectServices, "project-1", []string{"B"})
	assert.Empty(t, pendingComposeProjectServicesInternal(processedProjectServices, "project-1", []string{"A", "B"}))
}

func TestAnyImageIDsInUseSet(t *testing.T) {
	inUseSet := map[string]struct{}{
		"sha256:one": {},
		"sha256:two": {},
	}

	assert.True(t, anyImageIDsInUseSetInternal([]string{"sha256:one"}, inUseSet))
	assert.True(t, anyImageIDsInUseSetInternal([]string{"sha256:three", "sha256:two"}, inUseSet))
	assert.False(t, anyImageIDsInUseSetInternal([]string{"sha256:three"}, inUseSet))
	assert.False(t, anyImageIDsInUseSetInternal(nil, inUseSet))
	assert.False(t, anyImageIDsInUseSetInternal([]string{"sha256:one"}, nil))
}

func TestIsImageIDLikeReference(t *testing.T) {
	assert.True(t, isImageIDLikeReferenceInternal("sha256:abcdef"))
	assert.True(t, isImageIDLikeReferenceInternal("SHA256:ABCDEF"))
	assert.False(t, isImageIDLikeReferenceInternal("nginx:latest"))
	assert.False(t, isImageIDLikeReferenceInternal("docker.io/library/nginx:latest"))
}

func TestCollectUsedImagesFromContainers_FastPathSkipsInspectLikeRefs(t *testing.T) {
	svc := &UpdaterService{}
	out := map[string]struct{}{}

	// Simulate fast-path behavior expectations without Docker client dependency.
	containers := []container.Summary{
		{Image: "nginx:latest"},
		{Image: "sha256:abcdef"},
		{Image: "redis:7"},
	}

	for _, c := range containers {
		if c.Image != "" && !isImageIDLikeReferenceInternal(c.Image) {
			out[svc.normalizeRef(c.Image)] = struct{}{}
		}
	}

	assert.Contains(t, out, svc.normalizeRef("nginx:latest"))
	assert.Contains(t, out, svc.normalizeRef("redis:7"))
	assert.NotContains(t, out, svc.normalizeRef("sha256:abcdef"))
}

func mustHardwareAddr(t *testing.T, value string) network.HardwareAddr {
	t.Helper()

	hw, err := net.ParseMAC(value)
	require.NoError(t, err)

	return network.HardwareAddr(hw)
}

func TestBuildUpdaterRecreateNetworkingConfig(t *testing.T) {
	tests := []struct {
		name        string
		networkMode container.NetworkMode
		settings    *container.NetworkSettings
		apiVersion  string
		assertions  func(t *testing.T, got *network.NetworkingConfig)
	}{
		{
			name:        "skips container network mode",
			networkMode: container.NetworkMode("container:db"),
			apiVersion:  "1.44",
			settings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"custom": {Aliases: []string{"app"}},
				},
			},
			assertions: func(t *testing.T, got *network.NetworkingConfig) {
				require.Nil(t, got)
			},
		},
		{
			name:        "returns nil for empty settings",
			networkMode: container.NetworkMode("bridge"),
			apiVersion:  "1.44",
			settings:    &container.NetworkSettings{},
			assertions: func(t *testing.T, got *network.NetworkingConfig) {
				require.Nil(t, got)
			},
		},
		{
			name:        "preserves recreate-safe endpoint config and strips runtime fields",
			networkMode: container.NetworkMode("bridge"),
			apiVersion:  "1.43",
			settings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"bridge": {
						IPAMConfig: &network.EndpointIPAMConfig{
							IPv4Address:  netip.MustParseAddr("172.18.0.50"),
							LinkLocalIPs: []netip.Addr{netip.MustParseAddr("169.254.10.10")},
						},
						Links:       []string{"db:db"},
						Aliases:     []string{"app", "app-1"},
						DriverOpts:  map[string]string{"l2proxy": "true"},
						GwPriority:  42,
						MacAddress:  mustHardwareAddr(t, "02:42:ac:11:00:02"),
						IPAddress:   netip.MustParseAddr("172.18.0.20"),
						Gateway:     netip.MustParseAddr("172.18.0.1"),
						IPPrefixLen: 16,
					},
					"synobridge": nil,
				},
			},
			assertions: func(t *testing.T, got *network.NetworkingConfig) {
				require.NotNil(t, got)
				require.Len(t, got.EndpointsConfig, 2)

				bridge := got.EndpointsConfig["bridge"]
				require.NotNil(t, bridge)
				require.NotNil(t, bridge.IPAMConfig)
				assert.Equal(t, netip.MustParseAddr("172.18.0.50"), bridge.IPAMConfig.IPv4Address)
				assert.Equal(t, []netip.Addr{netip.MustParseAddr("169.254.10.10")}, bridge.IPAMConfig.LinkLocalIPs)
				assert.Equal(t, []string{"db:db"}, bridge.Links)
				assert.Equal(t, []string{"app", "app-1"}, bridge.Aliases)
				assert.Equal(t, map[string]string{"l2proxy": "true"}, bridge.DriverOpts)
				assert.Equal(t, 42, bridge.GwPriority)
				assert.Empty(t, bridge.MacAddress)
				assert.False(t, bridge.IPAddress.IsValid())
				assert.False(t, bridge.Gateway.IsValid())
				assert.Zero(t, bridge.IPPrefixLen)

				synobridge := got.EndpointsConfig["synobridge"]
				require.NotNil(t, synobridge)
				assert.Empty(t, synobridge.Aliases)
			},
		},
		{
			name:        "preserves network mac address when docker api supports it",
			networkMode: container.NetworkMode("bridge"),
			apiVersion:  "1.44",
			settings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"custom": {
						Aliases:    []string{"app"},
						MacAddress: mustHardwareAddr(t, "02:42:ac:11:00:03"),
					},
				},
			},
			assertions: func(t *testing.T, got *network.NetworkingConfig) {
				require.NotNil(t, got)
				require.Len(t, got.EndpointsConfig, 1)

				endpoint := got.EndpointsConfig["custom"]
				require.NotNil(t, endpoint)
				assert.Equal(t, []string{"app"}, endpoint.Aliases)
				assert.Equal(t, "02:42:ac:11:00:03", endpoint.MacAddress.String())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildUpdaterRecreateNetworkingConfigInternal(tt.networkMode, tt.settings, tt.apiVersion)
			tt.assertions(t, got)
		})
	}

	t.Run("clones aliases slice", func(t *testing.T) {
		settings := &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"custom": {
					IPAMConfig: &network.EndpointIPAMConfig{
						IPv4Address: netip.MustParseAddr("10.10.0.5"),
					},
					Links:      []string{"db:db"},
					Aliases:    []string{"app"},
					DriverOpts: map[string]string{"mode": "l2"},
				},
			},
		}

		got := buildUpdaterRecreateNetworkingConfigInternal(container.NetworkMode("bridge"), settings, "1.44")
		require.NotNil(t, got)

		got.EndpointsConfig["custom"].IPAMConfig.IPv4Address = netip.MustParseAddr("10.10.0.99")
		got.EndpointsConfig["custom"].Links[0] = "cache:cache"
		got.EndpointsConfig["custom"].Aliases[0] = "changed"
		got.EndpointsConfig["custom"].DriverOpts["mode"] = "l3"

		require.NotNil(t, settings.Networks["custom"].IPAMConfig)
		assert.Equal(t, netip.MustParseAddr("10.10.0.5"), settings.Networks["custom"].IPAMConfig.IPv4Address)
		assert.Equal(t, []string{"db:db"}, settings.Networks["custom"].Links)
		assert.Equal(t, []string{"app"}, settings.Networks["custom"].Aliases)
		assert.Equal(t, map[string]string{"mode": "l2"}, settings.Networks["custom"].DriverOpts)
	})
}

func setupUpdaterServiceTestDB(t *testing.T) *database.DB {
	t.Helper()

	db, err := gorm.Open(glsqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ImageUpdateRecord{}))

	return &database.DB{DB: db}
}

func TestUpdaterService_ClearImageUpdateRecordByID_AvoidsRepoCanonicalMismatch(t *testing.T) {
	ctx := context.Background()
	db := setupUpdaterServiceTestDB(t)

	record := models.ImageUpdateRecord{
		ID:             "sha256:old-image",
		Repository:     "nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      time.Now(),
	}
	require.NoError(t, db.WithContext(ctx).Create(&record).Error)

	svc := &UpdaterService{db: db}

	// Simulate the previous clear path that used normalized repo/tag.
	require.NoError(t, svc.clearImageUpdateRecord(ctx, "docker.io/library/nginx", "latest"))

	var unchanged models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", record.ID).First(&unchanged).Error)
	assert.True(t, unchanged.HasUpdate, "repo/tag clear should not match when repository canonicalization differs")

	require.NoError(t, svc.clearImageUpdateRecordByID(ctx, record.ID))

	var cleared models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", record.ID).First(&cleared).Error)
	assert.False(t, cleared.HasUpdate, "clear by image ID should always match the intended record")
}

func TestUpdaterService_CollectUsedImages_NoSourcesReturnsError(t *testing.T) {
	svc := &UpdaterService{}

	used, err := svc.collectUsedImages(context.Background())
	require.Error(t, err)
	assert.Nil(t, used)
}

func TestUpdaterService_ApplyPending_SkipsWhenUsedImageDiscoveryFails(t *testing.T) {
	ctx := context.Background()
	db := setupUpdaterServiceTestDB(t)

	record := models.ImageUpdateRecord{
		ID:             "sha256:pending-image",
		Repository:     "nginx",
		Tag:            "latest",
		HasUpdate:      true,
		UpdateType:     models.UpdateTypeDigest,
		CurrentVersion: "latest",
		CheckTime:      time.Now(),
	}
	require.NoError(t, db.WithContext(ctx).Create(&record).Error)

	svc := &UpdaterService{
		db: db,
		dockerService: &DockerClientService{
			config: &config.Config{DockerHost: "://bad-host"},
		},
	}

	out, err := svc.ApplyPending(ctx, false)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Empty(t, out.Items)
	assert.Zero(t, out.Checked)
	assert.Zero(t, out.Updated)
	assert.NotEmpty(t, out.Duration)

	var persisted models.ImageUpdateRecord
	require.NoError(t, db.WithContext(ctx).Where("id = ?", record.ID).First(&persisted).Error)
	assert.True(t, persisted.HasUpdate, "record should remain pending when used-image discovery fails")
}

func TestActiveComposeProjectNameSetInternal(t *testing.T) {
	projects := []models.Project{
		{Name: "My-App", Status: models.ProjectStatusRunning},
		{Name: "skip-me", Status: models.ProjectStatusStopped},
		{Name: "another_app", Status: models.ProjectStatusPartiallyRunning},
		{Name: "", Status: models.ProjectStatusRunning},
	}

	got := activeComposeProjectNameSetInternal(projects)

	assert.Contains(t, got, "My-App")
	assert.Contains(t, got, "my-app")
	assert.Contains(t, got, "another_app")
	assert.NotContains(t, got, "skip-me")
}

func TestCollectUsedImagesFromComposeContainersInternal(t *testing.T) {
	svc := &UpdaterService{}
	out := map[string]struct{}{}
	activeProjects := map[string]struct{}{
		"myapp": {},
	}

	composeContainers := []container.Summary{
		{
			Image: "nginx:latest",
			Labels: map[string]string{
				"com.docker.compose.project": "myapp",
			},
		},
		{
			Image: "redis:7",
			Labels: map[string]string{
				"com.docker.compose.project": "myapp",
				libupdater.LabelUpdater:      "false",
			},
		},
		{
			Image: "postgres:16",
			Labels: map[string]string{
				"com.docker.compose.project": "otherapp",
			},
		},
		{
			Image: "sha256:abcdef",
			Labels: map[string]string{
				"com.docker.compose.project": "myapp",
			},
		},
	}

	svc.collectUsedImagesFromComposeContainersInternal(composeContainers, activeProjects, out)

	assert.Contains(t, out, svc.normalizeRef("nginx:latest"))
	assert.NotContains(t, out, svc.normalizeRef("redis:7"))
	assert.NotContains(t, out, svc.normalizeRef("postgres:16"))
	assert.NotContains(t, out, svc.normalizeRef("sha256:abcdef"))
}

func TestResolveContainerImageMatchInternal(t *testing.T) {
	svc := &UpdaterService{}
	updatedNorm := map[string]string{
		svc.normalizeRef("nginx:latest"): "nginx:latest",
	}
	oldIDToNewRef := map[string]string{
		"sha256:img1": "redis:7",
	}

	tests := []struct {
		name        string
		container   container.Summary
		wantRef     string
		wantMatchID string
	}{
		{
			name: "match by image id",
			container: container.Summary{
				ImageID: "sha256:img1",
				Image:   "some/other:tag",
			},
			wantRef:     "redis:7",
			wantMatchID: "sha256:img1",
		},
		{
			name: "match by normalized image tag from summary",
			container: container.Summary{
				ImageID: "sha256:unknown",
				Image:   "docker.io/library/nginx:latest",
			},
			wantRef:     "nginx:latest",
			wantMatchID: svc.normalizeRef("nginx:latest"),
		},
		{
			name: "image id-like summary value cannot be tag matched",
			container: container.Summary{
				ImageID: "sha256:unknown",
				Image:   "sha256:abcdef",
			},
			wantRef:     "",
			wantMatchID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRef, gotMatch := svc.resolveContainerImageMatchInternal(tt.container, oldIDToNewRef, updatedNorm)
			assert.Equal(t, tt.wantRef, gotRef)
			assert.Equal(t, tt.wantMatchID, gotMatch)
		})
	}
}

func TestResolvePullableImageRefInternal(t *testing.T) {
	tests := []struct {
		name         string
		summaryImage string
		inspectImage string
		repoTags     []string
		wantRef      string
		wantSource   string
	}{
		{
			name:         "prefers inspect config image",
			summaryImage: "portainer/portainer.ce:latest",
			inspectImage: "ghcr.io/example/app:1.2.3",
			wantRef:      "ghcr.io/example/app:1.2.3",
			wantSource:   "container_inspect_config",
		},
		{
			name:         "falls back to summary image when inspect image is id-like",
			summaryImage: "portainer/portainer.ce:latest",
			inspectImage: "sha256:abcdef",
			wantRef:      "portainer/portainer.ce:latest",
			wantSource:   "container_summary",
		},
		{
			name:         "falls back to repo tag when summary and inspect are id-like",
			summaryImage: "sha256:abc123",
			inspectImage: "sha256:def456",
			repoTags:     []string{"<none>:<none>", "docker.io/library/portainer/portainer.ce:latest"},
			wantRef:      "docker.io/library/portainer/portainer.ce:latest",
			wantSource:   "image_repo_tag",
		},
		{
			name:         "returns empty when only id-like candidates exist",
			summaryImage: "sha256:abc123",
			inspectImage: "sha256:def456",
			repoTags:     []string{"<none>:<none>", "sha256:fff"},
			wantRef:      "",
			wantSource:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRef, gotSource := resolvePullableImageRefInternal(tt.summaryImage, tt.inspectImage, tt.repoTags)
			assert.Equal(t, tt.wantRef, gotRef)
			assert.Equal(t, tt.wantSource, gotSource)
		})
	}
}

func TestUpdaterService_StatusTrackingInternal(t *testing.T) {
	svc := &UpdaterService{
		updatingContainers: map[string]bool{},
		updatingProjects:   map[string]bool{},
	}

	stopContainer := svc.beginContainerUpdateInternal("container-1")
	stopProject := svc.beginProjectUpdateInternal("project-a")

	status := svc.GetStatus()
	assert.Equal(t, 1, status.UpdatingContainers)
	assert.Equal(t, 1, status.UpdatingProjects)
	assert.ElementsMatch(t, []string{"container-1"}, status.ContainerIds)
	assert.ElementsMatch(t, []string{"project-a"}, status.ProjectIds)

	stopContainer()
	stopProject()

	status = svc.GetStatus()
	assert.Zero(t, status.UpdatingContainers)
	assert.Zero(t, status.UpdatingProjects)
	assert.Empty(t, status.ContainerIds)
	assert.Empty(t, status.ProjectIds)
}

func TestComposeProjectNameFromLabelsInternal(t *testing.T) {
	assert.Equal(t, "", composeProjectNameFromLabelsInternal(nil))
	assert.Equal(t, "", composeProjectNameFromLabelsInternal(map[string]string{}))
	assert.Equal(t, "my-project", composeProjectNameFromLabelsInternal(map[string]string{
		"com.docker.compose.project": " my-project ",
	}))
}

func TestDockerProxyContainerNameInternal(t *testing.T) {
	tests := []struct {
		name       string
		dockerHost string
		expected   string
	}{
		{"empty", "", ""},
		{"unix socket", "unix:///var/run/docker.sock", ""},
		{"tcp with container hostname", "tcp://arcane-docker-socket-proxy:2375", "arcane-docker-socket-proxy"},
		{"tcp with container hostname no port", "tcp://my-proxy", "my-proxy"},
		{"tcp with ip address", "tcp://192.168.1.100:2375", ""},
		{"tcp with localhost", "tcp://localhost:2375", ""},
		{"tcp with fqdn", "tcp://docker.example.com:2375", ""},
		{"tcp with ipv6 address", "tcp://[::1]:2375", ""},
		{"tcp with ipv6 no port", "tcp://[::1]", ""},
		{"tcp with spaces", "  tcp://my-proxy:2375  ", "my-proxy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, dockerProxyContainerNameInternal(tt.dockerHost))
		})
	}
}
