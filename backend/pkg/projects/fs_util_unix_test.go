//go:build unix

package projects

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverProjectDirectories_UnreadableRootErrorIsActionable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are not enforced")
	}

	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o000))
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	_, err := DiscoverProjectDirectories(t.Context(), root, false, 0)
	require.Error(t, err)
	// The bare "permission denied" must be wrapped with the runtime identity
	// and remediation hints (#3489).
	assert.Contains(t, err.Error(), "not readable by the runtime user")
	assert.Contains(t, err.Error(), "PUID")
}
