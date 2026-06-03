package projects

import (
	"context"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/pkg/utils"
	"github.com/stretchr/testify/require"
)

func TestDetachFromHTTPContextInternal(t *testing.T) {
	t.Run("survives parent cancellation", func(t *testing.T) {
		parent, parentCancel := context.WithCancel(context.Background())
		detached, detachedCancel := detachFromHTTPContextInternal(parent)
		defer detachedCancel()

		// Cancel the parent (simulates HTTP request ending).
		parentCancel()

		// The detached context must still be alive.
		require.NoError(t, detached.Err())

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		require.False(t, deadline.IsZero())
	})

	t.Run("preserves context values", func(t *testing.T) {
		type testKey struct{}
		parent := context.WithValue(context.Background(), testKey{}, "hello")
		detached, cancel := detachFromHTTPContextInternal(parent)
		defer cancel()

		require.Equal(t, "hello", detached.Value(testKey{}))
	})

	t.Run("has its own deadline", func(t *testing.T) {
		detached, cancel := detachFromHTTPContextInternal(context.Background())
		defer cancel()

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		require.False(t, deadline.IsZero())
	})

	t.Run("survives parent deadline expiry", func(t *testing.T) {
		parent, parentCancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer parentCancel()

		time.Sleep(5 * time.Millisecond) // ensure parent deadline has passed

		detached, detachedCancel := detachFromHTTPContextInternal(parent)
		defer detachedCancel()

		require.NoError(t, detached.Err())

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		require.InDelta(t, float64(defaultComposeTimeout), float64(time.Until(deadline)), float64(5*time.Second))
	})

	t.Run("app lifecycle context cancels detached work on shutdown", func(t *testing.T) {
		appCtx, cancelApp := context.WithCancel(utils.WithAppLifecycleContext(context.Background()))
		detached, detachedCancel := detachFromHTTPContextInternal(appCtx)
		defer detachedCancel()

		cancelApp()

		require.ErrorIs(t, detached.Err(), context.Canceled)
	})
}

func TestComposeStopSkipsWhenNoServicesSpecified(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:9")

	err := ComposeStop(context.Background(), &composetypes.Project{Name: "test"}, nil)
	require.NoError(t, err)

	err = ComposeStop(context.Background(), &composetypes.Project{Name: "test"}, []string{})
	require.NoError(t, err)
}

func TestComposeUpOptions_RemoveOrphans(t *testing.T) {
	proj := &composetypes.Project{Name: "test"}

	t.Run("removeOrphans true propagates to CreateOptions", func(t *testing.T) {
		upOptions, _ := composeUpOptions(proj, nil, true, false)
		require.True(t, upOptions.RemoveOrphans)
	})

	t.Run("removeOrphans false leaves CreateOptions disabled", func(t *testing.T) {
		upOptions, _ := composeUpOptions(proj, nil, false, false)
		require.False(t, upOptions.RemoveOrphans)
	})

	t.Run("removeOrphans is independent of forceRecreate", func(t *testing.T) {
		// forceRecreate drives the Recreate policy, not RemoveOrphans.
		upOptions, _ := composeUpOptions(proj, nil, true, true)
		require.True(t, upOptions.RemoveOrphans)
		require.Equal(t, api.RecreateForce, upOptions.Recreate)

		upOptions, _ = composeUpOptions(proj, nil, false, true)
		require.False(t, upOptions.RemoveOrphans)
		require.Equal(t, api.RecreateForce, upOptions.Recreate)
	})
}
