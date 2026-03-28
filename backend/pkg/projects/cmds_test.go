package projects

import (
	"context"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
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
}

func TestComposeStopSkipsWhenNoServicesSpecified(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:9")

	err := ComposeStop(context.Background(), &composetypes.Project{Name: "test"}, nil)
	require.NoError(t, err)

	err = ComposeStop(context.Background(), &composetypes.Project{Name: "test"}, []string{})
	require.NoError(t, err)
}

func TestFilterProjectServicesForPullInternalReturnsDeepCopiedProject(t *testing.T) {
	project := &composetypes.Project{
		Name: "demo",
		Services: composetypes.Services{
			"web": {
				Name: "web",
				DependsOn: composetypes.DependsOnConfig{
					"db": {Condition: "service_started"},
				},
			},
			"db": {
				Name: "db",
			},
		},
		Networks: composetypes.Networks{
			"default": composetypes.NetworkConfig{
				Name: "demo_default",
			},
		},
	}

	filtered, err := filterProjectServicesForPullInternal(project, []string{"web"})
	require.NoError(t, err)
	require.NotNil(t, filtered)

	require.Contains(t, filtered.Services, "web")
	require.NotContains(t, filtered.Services, "db")

	web := filtered.Services["web"]
	delete(web.DependsOn, "db")
	filtered.Services["web"] = web
	delete(filtered.Networks, "default")

	require.Contains(t, project.Services["web"].DependsOn, "db")
	require.Contains(t, project.Networks, "default")
}
