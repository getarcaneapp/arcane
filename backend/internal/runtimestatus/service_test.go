package runtimestatus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"getarcane.dev/arcane/v3/pkg/authz"
	"getarcane.dev/types/v3"
)

func newTestTracker() *tracker {
	return &tracker{
		wake:      make(chan struct{}, 1),
		pending:   map[string]bool{},
		sessions:  map[string]string{},
		revisions: map[string]types.EnvironmentChanges{},
		statuses:  map[string]types.EnvironmentRuntimeStatus{},
	}
}

func TestStatusMayHaveChanged(t *testing.T) {
	previous := types.EnvironmentChanges{
		EnvironmentID: "env-1",
		Epoch:         "a",
		Kinds:         map[string]uint64{"container": 1, "swarm": 1},
	}
	tests := []struct {
		name           string
		current        types.EnvironmentChanges
		podmanDetected bool
		want           bool
	}{
		{
			name:    "unchanged",
			current: previous,
		},
		{
			name: "epoch changed",
			current: types.EnvironmentChanges{
				EnvironmentID: "env-1",
				Epoch:         "b",
				Kinds:         previous.Kinds,
			},
			want: true,
		},
		{
			name: "swarm changed",
			current: types.EnvironmentChanges{
				EnvironmentID: "env-1",
				Epoch:         "a",
				Kinds:         map[string]uint64{"container": 1, "swarm": 2},
			},
			want: true,
		},
		{
			name: "agent reconnected",
			current: types.EnvironmentChanges{
				EnvironmentID: "env-1",
				Epoch:         "a",
				Kinds:         map[string]uint64{"container": 1, "swarm": 1, "all": 3},
			},
			want: true,
		},
		{
			name: "container change on docker",
			current: types.EnvironmentChanges{
				EnvironmentID: "env-1",
				Epoch:         "a",
				Kinds:         map[string]uint64{"container": 2, "swarm": 1},
			},
		},
		{
			name: "container change on podman",
			current: types.EnvironmentChanges{
				EnvironmentID: "env-1",
				Epoch:         "a",
				Kinds:         map[string]uint64{"container": 2, "swarm": 1},
			},
			podmanDetected: true,
			want:           true,
		},
		{
			name: "pod change on podman",
			current: types.EnvironmentChanges{
				EnvironmentID: "env-1",
				Epoch:         "a",
				Kinds:         map[string]uint64{"container": 1, "swarm": 1, "podman": 4},
			},
			podmanDetected: true,
			want:           true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, statusMayHaveChanged(previous, tt.current, tt.podmanDetected))
		})
	}
}

func TestTrackerObserveQueuesTrackedEnvironments(t *testing.T) {
	tr := newTestTracker()
	tr.revisions["env-1"] = types.EnvironmentChanges{
		EnvironmentID: "env-1",
		Epoch:         "a",
		Kinds:         map[string]uint64{"swarm": 1},
	}

	tr.observe(types.EnvironmentChanges{
		EnvironmentID: "env-2",
		Epoch:         "a",
		Kinds:         map[string]uint64{"swarm": 5},
	})
	assert.Empty(t, tr.pending)

	tr.observe(types.EnvironmentChanges{
		EnvironmentID: "env-1",
		Epoch:         "a",
		Kinds:         map[string]uint64{"swarm": 1, "image": 2},
	})
	assert.Empty(t, tr.pending)

	tr.observe(types.EnvironmentChanges{
		EnvironmentID: "env-1",
		Epoch:         "a",
		Kinds:         map[string]uint64{"swarm": 3, "image": 2},
	})
	assert.Equal(t, map[string]bool{"env-1": true}, tr.pending)
	require.Len(t, tr.wake, 1)
	assert.Equal(t, uint64(3), tr.revisions["env-1"].Kinds["swarm"])
}

func TestTrackerStoreKeepsLastKnownStatus(t *testing.T) {
	tr := newTestTracker()
	tr.sessions["0"] = ""
	tr.sessions["env-1"] = "session"
	tr.statuses["env-1"] = types.EnvironmentRuntimeStatus{
		Swarm:         &types.SwarmRuntimeStatus{Enabled: true},
		EnvironmentID: "env-1",
	}

	tr.store(types.EnvironmentRuntimeStatus{
		Swarm:         &types.SwarmRuntimeStatus{},
		Podman:        &types.PodmanStatus{Detected: true},
		EnvironmentID: "0",
	})
	tr.store(types.EnvironmentRuntimeStatus{EnvironmentID: "env-1"})
	tr.store(types.EnvironmentRuntimeStatus{
		Swarm:         &types.SwarmRuntimeStatus{Enabled: true},
		EnvironmentID: "env-gone",
	})

	assert.Equal(t, []types.EnvironmentRuntimeStatus{
		{
			Swarm:         &types.SwarmRuntimeStatus{},
			Podman:        &types.PodmanStatus{Detected: true},
			EnvironmentID: "0",
		},
		{
			Swarm:         &types.SwarmRuntimeStatus{Enabled: true},
			EnvironmentID: "env-1",
		},
	}, tr.snapshotLocked())
}

func TestVisibleStatusesFiltersByPermission(t *testing.T) {
	statuses := []types.EnvironmentRuntimeStatus{
		{
			Swarm:         &types.SwarmRuntimeStatus{Enabled: true},
			Podman:        &types.PodmanStatus{Detected: true},
			EnvironmentID: "0",
		},
		{
			Swarm:         &types.SwarmRuntimeStatus{},
			Podman:        &types.PodmanStatus{},
			EnvironmentID: "env-1",
		},
		{
			Swarm:         &types.SwarmRuntimeStatus{Enabled: true},
			EnvironmentID: "env-2",
		},
	}
	permissions := authz.NewPermissionSet()
	permissions.AddEnv("0", authz.PermSwarmRead)
	permissions.AddEnv("env-1", authz.PermPodmanRead)

	assert.Equal(t, []types.EnvironmentRuntimeStatus{
		{
			Swarm:         &types.SwarmRuntimeStatus{Enabled: true},
			EnvironmentID: "0",
		},
		{
			Podman:        &types.PodmanStatus{},
			EnvironmentID: "env-1",
		},
	}, visibleStatuses(statuses, permissions))
	assert.Empty(t, visibleStatuses(statuses, nil))
	assert.Equal(t, statuses, visibleStatuses(statuses, authz.SudoPermissionSet()))
}
