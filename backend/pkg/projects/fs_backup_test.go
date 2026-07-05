package projects

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreProjectUpdateBackup_UndoesRenameWhenSourceWasRecreated(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "a", "keep.txt"), []byte("keep\n"), 0o644))

	// Batch: rename a -> b, create_folder a, then a later change fails.
	scope := ProjectUpdateBackupScope{
		Paths:       []string{"a"},
		RenamedDirs: [][2]string{{"a", "b"}},
	}
	backup, err := BackupProjectUpdateScope(projectDir, t.TempDir(), scope)
	require.NoError(t, err)

	require.NoError(t, os.Rename(filepath.Join(projectDir, "a"), filepath.Join(projectDir, "b")))
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "a"), 0o755))

	require.NoError(t, RestoreProjectUpdateBackup(projectDir, backup))

	require.NoDirExists(t, filepath.Join(projectDir, "b"))
	require.FileExists(t, filepath.Join(projectDir, "a", "keep.txt"))
}
