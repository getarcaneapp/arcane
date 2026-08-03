//go:build unix

package projects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reproduces issue #3309: a project directory containing a bind-mount data
// folder Arcane cannot read must still list everything else. Before the fix a
// single EACCES from fs.WalkDir aborted the walk, so the whole tree (and the
// revision that gates file edits) came back empty.
func TestReadProjectFileTree_SkipsUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits are ignored when running as root")
	}

	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte("include: []\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "includes"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "includes", "networks.yaml"), []byte("networks: {}\n"), 0o644))

	workdir := filepath.Join(projectDir, "volumes", "adguard-home", "workdir")
	require.NoError(t, os.MkdirAll(workdir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workdir, "data.db"), []byte("opaque"), 0o644))
	require.NoError(t, os.Chmod(workdir, 0o000))
	// Restore before t.TempDir's own cleanup runs, or it cannot remove the tree.
	t.Cleanup(func() { _ = os.Chmod(workdir, 0o755) })

	files, revision, _, err := ReadProjectFileTree(projectDir, 5, "", "compose.yaml", 0)
	require.NoError(t, err)
	require.NotEmpty(t, revision, "an empty revision blocks every project file edit")

	relativePaths := make([]string, 0, len(files))
	for _, file := range files {
		relativePaths = append(relativePaths, file.RelativePath)
	}

	// Siblings of the unreadable directory survive, and the directory itself is
	// still listed - just without children.
	assert.ElementsMatch(t, []string{
		"includes",
		"includes/networks.yaml",
		"volumes",
		"volumes/adguard-home",
		"volumes/adguard-home/workdir",
	}, relativePaths)

	// The compare walk in ApplyProjectFileChanges skips identically, so the
	// optimistic-concurrency check does not spuriously conflict.
	_, again, _, err := ReadProjectFileTree(projectDir, 5, "", "compose.yaml", 0)
	require.NoError(t, err)
	assert.Equal(t, revision, again)
}
