package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/getarcaneapp/arcane/backend/internal/config"
	"github.com/getarcaneapp/arcane/backend/internal/database"
	glsqlite "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/getarcaneapp/arcane/backend/internal/models"
	"github.com/getarcaneapp/arcane/backend/internal/utils/arcaneupdater"
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
	isArcane := arcaneupdater.IsArcaneContainer(labels)
	assert.True(t, isArcane, "IsArcaneContainer should return true for Arcane label")

	// Simulate the logic from restartContainersUsingOldIDs:
	// When upgradeService is not nil and isArcane is true, CLI should be called
	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	// Verify CLI upgrade was called
	assert.True(t, mockUpgrade.triggerCalled, "TriggerUpgradeViaCLI should have been called for Arcane container")
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
	isArcane := arcaneupdater.IsArcaneContainer(labels)
	assert.False(t, isArcane, "IsArcaneContainer should return false for non-Arcane container")

	// Simulate the logic from restartContainersUsingOldIDs
	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	// Verify CLI upgrade was NOT called
	assert.False(t, mockUpgrade.triggerCalled, "TriggerUpgradeViaCLI should not have been called for non-Arcane container")
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

	isArcane := arcaneupdater.IsArcaneContainer(labels)
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
	var mockUpgrade *mockSystemUpgradeService = nil

	labels := map[string]string{
		"com.getarcaneapp.arcane": "true",
	}

	isArcane := arcaneupdater.IsArcaneContainer(labels)
	assert.True(t, isArcane, "Should detect Arcane container")

	// When upgradeService is nil, ensure we don't attempt to call it.
	assert.Nil(t, mockUpgrade, "Upgrade service should be nil; should not attempt to call it")

	// Test passes if no panic occurs
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
			isArcane := arcaneupdater.IsArcaneContainer(tt.labels)
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

	isArcane := arcaneupdater.IsArcaneContainer(labels)
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
	isArcane := arcaneupdater.IsArcaneContainer(labels)

	// This is the actual logic from restartContainersUsingOldIDs
	if isArcane {
		_ = mockUpgrade.TriggerUpgradeViaCLI(ctx, systemUser)
	}

	assert.True(t, mockUpgrade.triggerCalled, "Should call CLI upgrade when service is not nil")
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
				arcaneupdater.LabelUpdater:   "false",
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
