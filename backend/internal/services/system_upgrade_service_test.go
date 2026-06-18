package services

import (
	"context"
	"errors"
	"testing"

	"github.com/getarcaneapp/arcane/backend/v2/internal/common"
	containertypes "github.com/moby/moby/api/types/container"
	mounttypes "github.com/moby/moby/api/types/mount"
	networktypes "github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	libupdater "go.getarcane.app/updater/pkg/labels"
)

// TestSystemUpgradeService_UpgradeFlag tests the upgrading flag behavior
func TestSystemUpgradeService_UpgradeFlag(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

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
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
	// Services can be nil in this test since we're just testing initialization
}

// TestSystemUpgradeService_ErrorVariables tests that error variables are properly defined
func TestSystemUpgradeService_ErrorVariables(t *testing.T) {
	// Test that all expected errors exist and are not nil
	require.Error(t, &common.NotRunningInDockerError{})
	require.Error(t, &common.ArcaneContainerNotFoundError{})
	require.Error(t, &common.UpgradeInProgressError{})
	require.Error(t, &common.DockerSocketAccessError{})

	// Test error messages
	require.Equal(t, "arcane is not running in a Docker container", (&common.NotRunningInDockerError{}).Error())
	require.Equal(t, "could not find Arcane container", (&common.ArcaneContainerNotFoundError{}).Error())
	require.Equal(t, "an upgrade is already in progress", (&common.UpgradeInProgressError{}).Error())
	require.Equal(t, "docker socket is not accessible", (&common.DockerSocketAccessError{}).Error())
}

// TestSystemUpgradeService_UpgradingFlag_ConcurrentAccess tests upgrading flag
func TestSystemUpgradeService_UpgradingFlag_ConcurrentAccess(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

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
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

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
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

	// Verify service is created and initialized properly
	require.NotNil(t, s)
	require.False(t, s.upgrading.Load())
}

// TestSystemUpgradeService_ConcurrentUpgradeAttempts tests that concurrent upgrade attempts are prevented
func TestSystemUpgradeService_ConcurrentUpgradeAttempts(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

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
	// The handler uses common.IsUpgradeInProgressError for conflict detection.

	err := &common.UpgradeInProgressError{}
	require.Equal(t, "an upgrade is already in progress", err.Error())

	require.True(t, common.IsUpgradeInProgressError(err))
}

// TestSystemUpgradeService_AtomicOperations tests atomic.Bool operations
func TestSystemUpgradeService_AtomicOperations(t *testing.T) {
	s := NewSystemUpgradeService(nil, nil, nil, nil, nil)

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

func TestDetermineUpgradeBinaryPath(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name: "agent container uses agent binary",
			labels: map[string]string{
				libupdater.LabelArcaneAgent: "true",
			},
			want: "/app/arcane-agent",
		},
		{
			name: "main container uses main binary",
			labels: map[string]string{
				libupdater.LabelArcane: "true",
			},
			want: "/app/arcane",
		},
		{
			name: "no labels defaults to main binary",
			want: "/app/arcane",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, determineUpgradeBinaryPathInternal(tt.labels))
		})
	}
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_TCPDockerHost(t *testing.T) {
	currentContainer := &containertypes.InspectResponse{
		HostConfig: &containertypes.HostConfig{NetworkMode: "bridge"},
		NetworkSettings: &containertypes.NetworkSettings{
			Networks: map[string]*networktypes.EndpointSettings{
				"bridge":      {},
				"arcane-test": {},
			},
		},
	}

	options, err := resolveSystemUpgraderRuntimeOptionsInternal(
		context.Background(),
		"tcp://docker-socket-proxy:2375",
		currentContainer,
		nil,
		func() bool { return true },
	)
	require.NoError(t, err)
	require.Equal(t, []string{"DOCKER_HOST=tcp://docker-socket-proxy:2375"}, options.ContainerEnv)
	require.Empty(t, options.Mounts)
	require.Equal(t, containertypes.NetworkMode("arcane-test"), options.NetworkMode)
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_UnixDockerHost(t *testing.T) {
	options, err := resolveSystemUpgraderRuntimeOptionsInternal(
		context.Background(),
		"unix:///var/run/docker.sock",
		nil,
		func(context.Context, string) (string, error) {
			return "/host/run/docker.sock", nil
		},
		func() bool { return true },
	)
	require.NoError(t, err)
	require.Equal(t, []string{"DOCKER_HOST=unix:///var/run/docker.sock"}, options.ContainerEnv)
	require.Equal(t, containertypes.NetworkMode(""), options.NetworkMode)
	require.Equal(t, []mounttypes.Mount{
		{
			Type:   mounttypes.TypeBind,
			Source: "/host/run/docker.sock",
			Target: "/var/run/docker.sock",
		},
	}, options.Mounts)
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_DefaultDockerHost(t *testing.T) {
	options, err := resolveSystemUpgraderRuntimeOptionsInternal(
		context.Background(),
		"",
		nil,
		func(context.Context, string) (string, error) {
			return "/var/run/docker.sock", nil
		},
		func() bool { return true },
	)
	require.NoError(t, err)
	require.Nil(t, options.ContainerEnv)
	require.Equal(t, []mounttypes.Mount{
		{
			Type:   mounttypes.TypeBind,
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		},
	}, options.Mounts)
}

func TestResolveSystemUpgraderRuntimeOptionsInternal_UnixDockerHostResolutionError(t *testing.T) {
	_, err := resolveSystemUpgraderRuntimeOptionsInternal(
		context.Background(),
		"unix:///var/run/docker.sock",
		nil,
		func(context.Context, string) (string, error) {
			return "", errors.New("inspect failed")
		},
		func() bool { return true },
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve unix socket source")
}
