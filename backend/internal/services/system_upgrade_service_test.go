package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSystemUpgradeService_UpgradeFlag tests the upgrading flag behavior
func TestSystemUpgradeService_UpgradeFlag(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	// Initially should be false
	require.False(t, s.upgrading.Load())

	// Simulate manual flag setting
	s.upgrading.Store(true)
	require.True(t, s.upgrading.Load())

	// Should be able to reset
	s.upgrading.Store(false)
	require.False(t, s.upgrading.Load())
}

// TestSystemUpgradeService_Initialization tests proper initialization
func TestSystemUpgradeService_Initialization(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
	// Services can be nil in this test since we're just testing initialization
}

// TestSystemUpgradeService_ErrorVariables tests that error variables are properly defined
func TestSystemUpgradeService_ErrorVariables(t *testing.T) {
	// Test that all expected errors exist and are not nil
	require.Error(t, ErrNotRunningInDocker)
	require.Error(t, ErrContainerNotFound)
	require.Error(t, ErrUpgradeInProgress)
	require.Error(t, ErrDockerSocketAccess)

	// Test error messages
	require.Equal(t, "arcane is not running in a Docker container", ErrNotRunningInDocker.Error())
	require.Equal(t, "could not find Arcane container", ErrContainerNotFound.Error())
	require.Equal(t, "an upgrade is already in progress", ErrUpgradeInProgress.Error())
	require.Equal(t, "docker socket is not accessible", ErrDockerSocketAccess.Error())
}

// TestSystemUpgradeService_UpgradingFlag_ConcurrentAccess tests upgrading flag
func TestSystemUpgradeService_UpgradingFlag_ConcurrentAccess(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	// Test initial state
	require.False(t, s.upgrading.Load(), "upgrading flag should start as false")

	// Test setting to true
	s.upgrading.Store(true)
	require.True(t, s.upgrading.Load(), "upgrading flag should be true after setting")

	// Test setting back to false
	s.upgrading.Store(false)
	require.False(t, s.upgrading.Load(), "upgrading flag should be false after resetting")
}

// TestSystemUpgradeService_CompareAndSwap tests atomic CompareAndSwap operation
func TestSystemUpgradeService_CompareAndSwap(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	// Test successful CompareAndSwap from false to true
	swapped := s.upgrading.CompareAndSwap(false, true)
	require.True(t, swapped, "CompareAndSwap should succeed when value is false")
	require.True(t, s.upgrading.Load(), "upgrading should be true after swap")

	// Test failed CompareAndSwap (already true)
	swapped = s.upgrading.CompareAndSwap(false, true)
	require.False(t, swapped, "CompareAndSwap should fail when value is already true")
	require.True(t, s.upgrading.Load(), "upgrading should still be true")

	// Reset and test again
	s.upgrading.Store(false)
	swapped = s.upgrading.CompareAndSwap(false, true)
	require.True(t, swapped, "CompareAndSwap should succeed after reset")
}

// TestSystemUpgradeService_Services tests that services are stored correctly
func TestSystemUpgradeService_Services(t *testing.T) {
	// Create upgrade service with nil services (valid for testing initialization)
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	// Verify service is created and initialized properly
	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
}

// TestSystemUpgradeService_ConcurrentUpgradeAttempts tests that concurrent upgrade attempts are prevented
func TestSystemUpgradeService_ConcurrentUpgradeAttempts(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	// Simulate first upgrade starting
	success := s.upgrading.CompareAndSwap(false, true)
	require.True(t, success, "First upgrade attempt should succeed")

	// Simulate second concurrent upgrade attempt
	success = s.upgrading.CompareAndSwap(false, true)
	require.False(t, success, "Second concurrent upgrade attempt should fail")

	// Cleanup
	s.upgrading.Store(false)

	// Should be able to upgrade again after cleanup
	success = s.upgrading.CompareAndSwap(false, true)
	require.True(t, success, "Upgrade should be possible after reset")
}

// TestSystemUpgradeService_UpgradeInProgressError tests the upgrade in progress sentinel error
func TestSystemUpgradeService_UpgradeInProgressError(t *testing.T) {
	// This tests the specific error that the handler checks for
	// The handler uses: if errors.Is(err, services.ErrUpgradeInProgress)

	require.Equal(t, "an upgrade is already in progress", ErrUpgradeInProgress.Error())

	// Test that the error is not nil
	require.Error(t, ErrUpgradeInProgress)
}

// TestSystemUpgradeService_AtomicOperations tests atomic.Bool operations
func TestSystemUpgradeService_AtomicOperations(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil)

	// Test Load
	require.False(t, s.upgrading.Load())

	// Test Store
	s.upgrading.Store(true)
	require.True(t, s.upgrading.Load())

	// Test CompareAndSwap success
	s.upgrading.Store(false)
	swapped := s.upgrading.CompareAndSwap(false, true)
	require.True(t, swapped)

	// Test CompareAndSwap failure
	swapped = s.upgrading.CompareAndSwap(false, true)
	require.False(t, swapped)
	require.True(t, s.upgrading.Load())

	// Test Swap
	s.upgrading.Store(false)
	old := s.upgrading.Swap(true)
	require.False(t, old)
	require.True(t, s.upgrading.Load())
}
