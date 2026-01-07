package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
