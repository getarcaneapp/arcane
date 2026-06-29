//go:build unix

package projects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscoverProjectDirectories_SkipsUnreadableSubdirectory verifies that an
// unreadable subdirectory (such as a root-owned lost+found on a mounted
// volume) is skipped during discovery instead of aborting the entire scan, so
// that sibling projects are still found.
func TestDiscoverProjectDirectories_SkipsUnreadableSubdirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	root := t.TempDir()

	// A valid project sits next to an unreadable system directory.
	writeComposeFileInternal(t, filepath.Join(root, "app"))

	unreadable := filepath.Join(root, "lost+found")
	require.NoError(t, os.MkdirAll(unreadable, 0o755))
	require.NoError(t, os.Chmod(unreadable, 0o000))
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	discovered, err := DiscoverProjectDirectories(root, false, 0)
	require.NoError(t, err)

	names := discoveredNamesInternal(discovered)
	assert.Equal(t, []string{"app"}, names,
		"the valid sibling project must be discovered even though a sibling directory is unreadable")
}
