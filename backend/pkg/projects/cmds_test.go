package projects

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/getarcaneapp/arcane/backend/v2/pkg/utils"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

func TestDetachFromHTTPContextInternal(t *testing.T) {
	t.Run("survives parent cancellation", func(t *testing.T) {
		parent, parentCancel := context.WithCancel(context.Background())
		detached, detachedCancel := detachFromHTTPContextInternal(parent, defaultComposeTimeout)
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
		detached, cancel := detachFromHTTPContextInternal(parent, defaultComposeTimeout)
		defer cancel()

		require.Equal(t, "hello", detached.Value(testKey{}))
	})

	t.Run("has its own deadline", func(t *testing.T) {
		detached, cancel := detachFromHTTPContextInternal(context.Background(), defaultComposeTimeout)
		defer cancel()

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		require.False(t, deadline.IsZero())
	})

	t.Run("survives parent deadline expiry", func(t *testing.T) {
		parent, parentCancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer parentCancel()

		time.Sleep(5 * time.Millisecond) // ensure parent deadline has passed

		detached, detachedCancel := detachFromHTTPContextInternal(parent, defaultComposeTimeout)
		defer detachedCancel()

		require.NoError(t, detached.Err())

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		require.InDelta(t, float64(defaultComposeTimeout), float64(time.Until(deadline)), float64(5*time.Second))
	})

	t.Run("deadline scales with requested timeout", func(t *testing.T) {
		detached, cancel := detachFromHTTPContextInternal(context.Background(), 2*time.Hour)
		defer cancel()

		deadline, ok := detached.Deadline()
		require.True(t, ok)
		require.InDelta(t, float64(2*time.Hour), float64(time.Until(deadline)), float64(5*time.Second))
	})

	t.Run("app lifecycle context cancels detached work on shutdown", func(t *testing.T) {
		appCtx, cancelApp := context.WithCancel(utils.WithAppLifecycleContext(context.Background()))
		detached, detachedCancel := detachFromHTTPContextInternal(appCtx, defaultComposeTimeout)
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
		upOptions, _ := composeUpOptions(proj, nil, true, false, 0, ComposeEnvOptions{})
		require.True(t, upOptions.RemoveOrphans)
	})

	t.Run("removeOrphans false leaves CreateOptions disabled", func(t *testing.T) {
		upOptions, _ := composeUpOptions(proj, nil, false, false, 0, ComposeEnvOptions{})
		require.False(t, upOptions.RemoveOrphans)
	})

	t.Run("removeOrphans is independent of forceRecreate", func(t *testing.T) {
		// forceRecreate drives the Recreate policy, not RemoveOrphans.
		upOptions, _ := composeUpOptions(proj, nil, true, true, 0, ComposeEnvOptions{})
		require.True(t, upOptions.RemoveOrphans)
		require.Equal(t, api.RecreateForce, upOptions.Recreate)

		upOptions, _ = composeUpOptions(proj, nil, false, true, 0, ComposeEnvOptions{})
		require.False(t, upOptions.RemoveOrphans)
		require.Equal(t, api.RecreateForce, upOptions.Recreate)
	})

	t.Run("COMPOSE_REMOVE_ORPHANS / COMPOSE_IGNORE_ORPHANS propagate", func(t *testing.T) {
		upOptions, _ := composeUpOptions(proj, nil, false, false, 0, ComposeEnvOptions{RemoveOrphans: true, IgnoreOrphans: true})
		require.True(t, upOptions.RemoveOrphans)
		require.True(t, upOptions.IgnoreOrphans)
	})
}

func TestListGlobalComposeContainersUsesProvidedClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/containers/json") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"Id":"abc123","Labels":{"com.docker.compose.project":"demo"}}]`)
	}))
	t.Cleanup(server.Close)

	apiClient, err := client.New(
		client.WithHost(server.URL),
		client.WithAPIVersion("1.41"),
		client.WithHTTPClient(server.Client()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = apiClient.Close() })

	// The bogus dockerHost proves the provided client wins over the
	// fallback host.
	containers, err := ListGlobalComposeContainers(context.Background(), apiClient, "tcp://unused.example.com:2375")
	require.NoError(t, err)
	require.Len(t, containers, 1)
	require.Equal(t, "abc123", containers[0].ID)
}
