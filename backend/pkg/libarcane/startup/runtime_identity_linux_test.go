//go:build linux

package startup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeIdentitySupplementaryGroups(t *testing.T) {
	t.Run("maps default docker socket group when docker host unset", func(t *testing.T) {
		groups := runtimeIdentitySupplementaryGroupsInternal(
			"",
			func(socketPath string) (uint32, bool) {
				require.Equal(t, defaultDockerSocketPath, socketPath)
				return 997, true
			},
		)

		require.Equal(t, []uint32{997}, groups)
	})

	t.Run("maps custom unix docker host socket group", func(t *testing.T) {
		groups := runtimeIdentitySupplementaryGroupsInternal(
			"unix:///tmp/docker.sock",
			func(socketPath string) (uint32, bool) {
				require.Equal(t, "/tmp/docker.sock", socketPath)
				return 998, true
			},
		)

		require.Equal(t, []uint32{998}, groups)
	})

	t.Run("skips non unix docker host", func(t *testing.T) {
		called := false

		groups := runtimeIdentitySupplementaryGroupsInternal(
			"tcp://docker:2375",
			func(string) (uint32, bool) {
				called = true
				return 0, false
			},
		)

		require.Nil(t, groups)
		require.False(t, called)
	})

	t.Run("skips socket group when socket lookup fails", func(t *testing.T) {
		groups := runtimeIdentitySupplementaryGroupsInternal(
			"unix:///tmp/missing.sock",
			func(string) (uint32, bool) { return 0, false },
		)

		require.Nil(t, groups)
	})
}
