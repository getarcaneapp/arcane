//go:build unix

package projects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListProjectWorkspaceFolder_SkipsUnreadableFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("include: []\n"), 0o644))
	workdir := filepath.Join(projectDir, "volumes", "workdir")
	require.NoError(t, os.MkdirAll(workdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, "data.db"), []byte("opaque"), 0o644))
	require.NoError(t, os.Chmod(workdir, 0o000))
	// Restore before t.TempDir's own cleanup runs, or it cannot remove the tree.
	t.Cleanup(func() { _ = os.Chmod(workdir, 0o755) })
	opts := ProjectWorkspaceListOptions{ComposeFileName: "compose.yaml", MaxEntries: 100, MaxFileSizeBytes: 1024}

	parent, err := ListProjectWorkspaceFolder(t.Context(), projectDir, "volumes", opts)
	require.NoError(t, err)
	require.Len(t, parent.Files, 1)
	assert.Equal(t, "volumes/workdir", parent.Files[0].RelativePath)

	unreadable, err := ListProjectWorkspaceFolder(t.Context(), projectDir, "volumes/workdir", opts)
	require.NoError(t, err)
	assert.Empty(t, unreadable.Files)
}
