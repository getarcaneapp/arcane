package swarm

import (
	"context"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/getarcaneapp/arcane/types/v3"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestCleanupStaleManagedResourcesMatchesCurrentAndLegacyLabels(t *testing.T) {
	t.Parallel()

	listCalls := 0
	removedIDs := make([]string, 0, 2)
	adapter := fileResourceAdapter{
		ResourceType: "config",
		List: func(_ context.Context, filters dockerclient.Filters) ([]staleManagedResource, error) {
			listCalls++
			labels := filters["label"]
			require.True(t, labels[types.SwarmStackNamespaceLabel+"=stack"])

			switch {
			case labels[resourceTypeLabel+"=config"]:
				return []staleManagedResource{
					{ID: "current", Name: "desired"},
					{ID: "both", Name: "stale-both"},
				}, nil
			case labels[legacyResourceTypeLabel+"=config"]:
				return []staleManagedResource{
					{ID: "legacy", Name: "stale-legacy"},
					{ID: "both", Name: "stale-both"},
				}, nil
			default:
				require.FailNow(t, "unexpected managed-resource label filter")
				return nil, nil
			}
		},
		Remove: func(_ context.Context, id string) error {
			removedIDs = append(removedIDs, id)
			return nil
		},
	}

	err := cleanupStaleManagedResources(
		t.Context(),
		"stack",
		map[string]struct{}{"desired": {}},
		adapter,
		true,
		true,
	)
	require.NoError(t, err)
	require.Equal(t, 2, listCalls)
	require.ElementsMatch(t, []string{"both", "legacy"}, removedIDs)
}

func TestCleanupStaleManagedResourcesFailsWhenResourceStaysInUse(t *testing.T) {
	previousBackoff := staleSwarmResourceRemoveBackoff
	staleSwarmResourceRemoveBackoff = time.Millisecond
	t.Cleanup(func() { staleSwarmResourceRemoveBackoff = previousBackoff })

	removeCalls := 0
	adapter := fileResourceAdapter{
		ResourceType: "config",
		List: func(_ context.Context, _ dockerclient.Filters) ([]staleManagedResource, error) {
			return []staleManagedResource{{ID: "stale", Name: "stale"}}, nil
		},
		Remove: func(_ context.Context, _ string) error {
			removeCalls++
			return cerrdefs.ErrConflict.WithMessage("config is in use by task")
		},
	}

	err := cleanupStaleManagedResources(t.Context(), "stack", map[string]struct{}{}, adapter, false, false)
	require.Error(t, err)
	require.Equal(t, staleSwarmResourceRemoveAttempts, removeCalls)

	removeCalls = 0
	err = cleanupStaleManagedResources(t.Context(), "stack", map[string]struct{}{}, adapter, false, true)
	require.NoError(t, err)
	require.Equal(t, 1, removeCalls)
}
